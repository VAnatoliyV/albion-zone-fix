package screen

import "image"

// Info — что снято: для журнала (тестер пришлёт — видно DPI и рамку).
type Info struct {
	Cursor image.Point     // курсор в пикселях экрана
	DPI    int             // масштаб монитора под курсором (96 = 100 %)
	Mon    image.Rectangle // монитор под курсором в пикселях (пусто — не узнали)
	Rect   image.Rectangle // снятая рамка в пикселях экрана
	Out    image.Point     // размер картинки для OCR
	Scale  int             // во сколько раз увеличено для OCR
	Empty  string          // EmptyBlack, EmptySame или "" — снимок похож на настоящий
	Tries  int             // сколько раз брали пиксели (GetDIBits/BitBlt с повтором)
}
