package paddle

import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"
)

// ONNX Runtime через C API без CGO: библиотека грузится во время работы
// (onnxruntime.dll рядом с программой, на маке для тестов —
// libonnxruntime.dylib), функции зовутся по указателям из таблицы OrtApi
// (call — в ort_windows.go через syscall, в ort_unix.go через purego).
// Номера функций в OrtApi постоянны между версиями (новые добавляются
// только в конец) — взяты из onnxruntime_c_api.h 1.30.

// apiVersion — какую версию таблицы OrtApi просим: все нужные функции
// есть с 1.16, новая библиотека старую версию таблицы отдаёт.
const apiVersion = 16

// Номера функций в OrtApi.
const (
	fnGetErrorMessage                      = 2
	fnCreateEnv                            = 3
	fnCreateSessionFromArray               = 8
	fnRun                                  = 9
	fnCreateSessionOptions                 = 10
	fnSetSessionExecutionMode              = 13
	fnDisableCpuMemArena                   = 19
	fnSetSessionGraphOptimizationLevel     = 23
	fnSetIntraOpNumThreads                 = 24
	fnSetInterOpNumThreads                 = 25
	fnSessionGetInputName                  = 36
	fnSessionGetOutputName                 = 37
	fnCreateTensorWithDataAsOrtValue       = 49
	fnGetTensorMutableData                 = 51
	fnGetDimensionsCount                   = 61
	fnGetDimensions                        = 62
	fnGetTensorTypeAndShape                = 65
	fnCreateCpuMemoryInfo                  = 69
	fnAllocatorFree                        = 76
	fnGetAllocatorWithDefaultOptions       = 78
	fnReleaseEnv                           = 92
	fnReleaseStatus                        = 93
	fnReleaseMemoryInfo                    = 94
	fnReleaseSession                       = 95
	fnReleaseValue                         = 96
	fnReleaseTensorTypeAndShapeInfo        = 99
	fnReleaseSessionOptions                = 100
	fnSessionGetModelMetadata              = 111
	fnModelMetadataLookupCustomMetadataMap = 116
	fnReleaseModelMetadata                 = 118
	fnAddSessionConfigEntry                = 130
)

// Значения перечислений C API.
const (
	logWarning     = 2 // ORT_LOGGING_LEVEL_WARNING
	logError       = 3
	allocArena     = 1  // OrtArenaAllocator
	memDefault     = 0  // OrtMemTypeDefault
	tensorFloat    = 1  // ONNX_TENSOR_ELEMENT_DATA_TYPE_FLOAT
	optAll         = 99 // ORT_ENABLE_ALL
	execSequential = 0  // ORT_SEQUENTIAL
)

// ptr — адрес из C как unsafe.Pointer (без предупреждения vet: память не
// Go, сборщик её не двигает).
func ptr(u uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&u)) }

// cstr — строка C (до нуля).
func cstr(p uintptr) string {
	if p == 0 {
		return ""
	}
	n := 0
	for *(*byte)(ptr(p + uintptr(n))) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(ptr(p)), n))
}

// cbytes — строка Go как строка C (с нулём на конце).
func cbytes(s string) []byte { return append([]byte(s), 0) }

// ort — загруженная библиотека.
type ort struct {
	api     uintptr // const OrtApi*
	Version string
}

// loadORT грузит библиотеку по полному пути (не по имени: в System32
// Windows 11 лежит своя, старая onnxruntime.dll).
func loadORT(path string) (*ort, error) {
	getBase, err := openSym(path, "OrtGetApiBase")
	if err != nil {
		return nil, err
	}
	base := call(getBase)
	if base == 0 {
		return nil, errors.New("OrtGetApiBase вернул пусто")
	}
	getAPI := *(*uintptr)(ptr(base))
	getVer := *(*uintptr)(ptr(base + unsafe.Sizeof(uintptr(0))))
	o := &ort{Version: cstr(call(getVer))}
	o.api = call(getAPI, apiVersion)
	if o.api == 0 {
		return nil, fmt.Errorf("onnxruntime %s не отдаёт API версии %d", o.Version, apiVersion)
	}
	return o, nil
}

func (o *ort) fn(i int) uintptr {
	return *(*uintptr)(ptr(o.api + uintptr(i)*unsafe.Sizeof(uintptr(0))))
}

// do зовёт функцию OrtApi, возвращающую OrtStatus*: не пусто — ошибка.
// Указатели на память Go — только как uintptr(unsafe.Pointer(…)) прямо в
// вызове: go:uintptrescapes держит их в куче и живыми до конца вызова.
//
//go:uintptrescapes
func (o *ort) do(what string, i int, args ...uintptr) error {
	st := call(o.fn(i), args...)
	if st == 0 {
		return nil
	}
	msg := cstr(call(o.fn(fnGetErrorMessage), st))
	call(o.fn(fnReleaseStatus), st)
	return fmt.Errorf("onnxruntime %s: %s", what, msg)
}

func (o *ort) release(i int, h uintptr) {
	if h != 0 {
		call(o.fn(i), h)
	}
}

// env — OrtEnv и описание памяти CPU для входных тензоров.
type env struct {
	o        *ort
	env, mem uintptr
	alloc    uintptr // распределитель по умолчанию (не освобождается)
}

func (o *ort) newEnv() (*env, error) {
	e := &env{o: o}
	id := cbytes("albion-journal")
	if err := o.do("CreateEnv", fnCreateEnv, logError, uintptr(unsafe.Pointer(&id[0])), uintptr(unsafe.Pointer(&e.env))); err != nil {
		return nil, err
	}
	runtime.KeepAlive(id)
	if err := o.do("CreateCpuMemoryInfo", fnCreateCpuMemoryInfo, allocArena, memDefault, uintptr(unsafe.Pointer(&e.mem))); err != nil {
		e.close()
		return nil, err
	}
	if err := o.do("GetAllocator", fnGetAllocatorWithDefaultOptions, uintptr(unsafe.Pointer(&e.alloc))); err != nil {
		e.close()
		return nil, err
	}
	return e, nil
}

func (e *env) close() {
	e.o.release(fnReleaseMemoryInfo, e.mem)
	e.o.release(fnReleaseEnv, e.env)
	e.mem, e.env = 0, 0
}

// session — модель распознавания строк.
type session struct {
	e       *env
	s       uintptr
	in, out []byte   // имена входа и выхода (строки C)
	chars   []string // словарь модели: 0 — пусто (blank CTC), дальше символы
}

// newSession загружает модель из памяти. threads — потоков на модель.
func (e *env) newSession(model []byte, threads int) (*session, error) {
	o := e.o
	var opts uintptr
	if err := o.do("CreateSessionOptions", fnCreateSessionOptions, uintptr(unsafe.Pointer(&opts))); err != nil {
		return nil, err
	}
	defer o.release(fnReleaseSessionOptions, opts)
	if err := o.do("SetIntraOpNumThreads", fnSetIntraOpNumThreads, opts, uintptr(threads)); err != nil {
		return nil, err
	}
	if err := o.do("SetInterOpNumThreads", fnSetInterOpNumThreads, opts, 1); err != nil {
		return nil, err
	}
	if err := o.do("SetSessionExecutionMode", fnSetSessionExecutionMode, opts, execSequential); err != nil {
		return nil, err
	}
	if err := o.do("SetSessionGraphOptimizationLevel", fnSetSessionGraphOptimizationLevel, opts, optAll); err != nil {
		return nil, err
	}
	// Ширина строк разная — арена копила бы память под каждую; потоки после
	// распознавания не крутятся вхолостую (рядом игра).
	if err := o.do("DisableCpuMemArena", fnDisableCpuMemArena, opts); err != nil {
		return nil, err
	}
	for _, kv := range [][2]string{
		{"session.intra_op.allow_spinning", "0"},
		{"session.inter_op.allow_spinning", "0"},
	} {
		k, v := cbytes(kv[0]), cbytes(kv[1])
		if err := o.do("AddSessionConfigEntry", fnAddSessionConfigEntry, opts, uintptr(unsafe.Pointer(&k[0])), uintptr(unsafe.Pointer(&v[0]))); err != nil {
			return nil, err
		}
		runtime.KeepAlive(k)
		runtime.KeepAlive(v)
	}
	s := &session{e: e}
	if err := o.do("CreateSession", fnCreateSessionFromArray, e.env, uintptr(unsafe.Pointer(&model[0])), uintptr(len(model)), opts, uintptr(unsafe.Pointer(&s.s))); err != nil {
		return nil, err
	}
	runtime.KeepAlive(model)
	name := func(what string, i int) ([]byte, error) {
		var p uintptr
		if err := o.do(what, i, s.s, 0, e.alloc, uintptr(unsafe.Pointer(&p))); err != nil {
			return nil, err
		}
		defer call(o.fn(fnAllocatorFree), e.alloc, p)
		return cbytes(cstr(p)), nil
	}
	var err error
	if s.in, err = name("SessionGetInputName", fnSessionGetInputName); err != nil {
		s.close()
		return nil, err
	}
	if s.out, err = name("SessionGetOutputName", fnSessionGetOutputName); err != nil {
		s.close()
		return nil, err
	}
	dict, err := s.meta("character")
	if err != nil {
		s.close()
		return nil, err
	}
	if s.chars = Charset(dict); len(s.chars) < 3 {
		s.close()
		return nil, errors.New("в модели нет словаря символов (character)")
	}
	return s, nil
}

// meta — строка из метаданных модели ("" — нет такой).
func (s *session) meta(key string) (string, error) {
	o, e := s.e.o, s.e
	var md uintptr
	if err := o.do("SessionGetModelMetadata", fnSessionGetModelMetadata, s.s, uintptr(unsafe.Pointer(&md))); err != nil {
		return "", err
	}
	defer o.release(fnReleaseModelMetadata, md)
	k := cbytes(key)
	var v uintptr
	if err := o.do("LookupCustomMetadataMap", fnModelMetadataLookupCustomMetadataMap, md, e.alloc, uintptr(unsafe.Pointer(&k[0])), uintptr(unsafe.Pointer(&v))); err != nil {
		return "", err
	}
	runtime.KeepAlive(k)
	if v == 0 {
		return "", nil
	}
	defer call(o.fn(fnAllocatorFree), e.alloc, v)
	return cstr(v), nil
}

func (s *session) close() {
	s.e.o.release(fnReleaseSession, s.s)
	s.s = 0
}

// run — распознать пачку n картинок 3×48×w (data — NCHW). Ответ —
// вероятности [n][шаги][символы] плоско и размеры.
func (s *session) run(data []float32, n, w int) ([]float32, []int64, error) {
	o, e := s.e.o, s.e
	shape := []int64{int64(n), 3, Height, int64(w)}
	var in uintptr
	if err := o.do("CreateTensor", fnCreateTensorWithDataAsOrtValue, e.mem, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)*4),
		uintptr(unsafe.Pointer(&shape[0])), uintptr(len(shape)), tensorFloat, uintptr(unsafe.Pointer(&in))); err != nil {
		return nil, nil, err
	}
	defer func() {
		o.release(fnReleaseValue, in)
		runtime.KeepAlive(data)
	}()
	inNames := []uintptr{uintptr(unsafe.Pointer(&s.in[0]))}
	outNames := []uintptr{uintptr(unsafe.Pointer(&s.out[0]))}
	ins := []uintptr{in}
	outs := []uintptr{0}
	if err := o.do("Run", fnRun, s.s, 0, uintptr(unsafe.Pointer(&inNames[0])), uintptr(unsafe.Pointer(&ins[0])), 1, uintptr(unsafe.Pointer(&outNames[0])), 1, uintptr(unsafe.Pointer(&outs[0]))); err != nil {
		return nil, nil, err
	}
	runtime.KeepAlive(s)
	out := outs[0]
	defer o.release(fnReleaseValue, out)
	var info uintptr
	if err := o.do("GetTensorTypeAndShape", fnGetTensorTypeAndShape, out, uintptr(unsafe.Pointer(&info))); err != nil {
		return nil, nil, err
	}
	defer o.release(fnReleaseTensorTypeAndShapeInfo, info)
	var cnt uintptr
	if err := o.do("GetDimensionsCount", fnGetDimensionsCount, info, uintptr(unsafe.Pointer(&cnt))); err != nil {
		return nil, nil, err
	}
	if cnt != 3 {
		return nil, nil, fmt.Errorf("выход модели: %d измерений, ждали 3", cnt)
	}
	dims := make([]int64, cnt)
	if err := o.do("GetDimensions", fnGetDimensions, info, uintptr(unsafe.Pointer(&dims[0])), cnt); err != nil {
		return nil, nil, err
	}
	total := int64(1)
	for _, d := range dims {
		if d <= 0 {
			return nil, nil, fmt.Errorf("выход модели: размер %v", dims)
		}
		total *= d
	}
	var p uintptr
	if err := o.do("GetTensorMutableData", fnGetTensorMutableData, out, uintptr(unsafe.Pointer(&p))); err != nil {
		return nil, nil, err
	}
	res := make([]float32, total)
	copy(res, unsafe.Slice((*float32)(ptr(p)), total))
	return res, dims, nil
}
