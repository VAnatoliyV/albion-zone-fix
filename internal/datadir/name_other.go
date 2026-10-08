//go:build !windows

package datadir

// На маке "Albion Journal" — каталог настоящего мак-приложения;
// встроенный разборщик стирает там файл места при запуске.
const dirName = Name + " (Windows)"
