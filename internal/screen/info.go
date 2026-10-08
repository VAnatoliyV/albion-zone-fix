package screen

import "image"

// Info — что снято: для журнала (тестер пришлёт — видно DPI и рамку).
type Info struct {
	Cursor image.Point     // курсор в пикселях экрана
	DPI    int             // масштаб монитора под курсором (96 = 100 %)
	Rect   image.Rectangle // снятая рамка в пикселях экрана
	Out    image.Point     // размер картинки для OCR
}
