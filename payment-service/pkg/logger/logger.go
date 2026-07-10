package logger

import "log"

func Info(format string, args ...any) {
	log.Printf(format, args...)
}
