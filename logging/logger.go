package logging

import (
	"fmt"
	"log"
	"os"
)

// ANSI color codes
const (
	Reset   = "\033[0m"
	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	White   = "\033[37m"
	Dim     = "\033[2m"
	Bold    = "\033[1m"
)

// Log tags with emojis
const (
	StreamTag        = "▶ [ST]"
	NonStreamTag     = "▶ [NS]"
	DoneTag          = "■ [ST]"
	DoneNonStreamTag = "■ [NS]"
	Separator        = "===================================="
)

// Logger wraps the standard logger with color support
type Logger struct {
	*log.Logger
}

// NewLogger creates a new Logger instance
func NewLogger() *Logger {
	return &Logger{
		Logger: log.New(os.Stdout, "", log.LstdFlags),
	}
}

// StreamLog logs streaming requests with blue color
func (l *Logger) StreamLog(format string, v ...interface{}) {
	message := fmt.Sprintf(format, v...)
	l.Printf("%s%s%s %s", Blue, StreamTag, Reset, message)
}

// NonStreamLog logs non-streaming requests with cyan color
func (l *Logger) NonStreamLog(format string, v ...interface{}) {
	message := fmt.Sprintf(format, v...)
	l.Printf("%s%s%s %s", Cyan, NonStreamTag, Reset, message)
}

// DoneLog logs streaming completions with green color
func (l *Logger) DoneLog(format string, v ...interface{}) {
	message := fmt.Sprintf(format, v...)
	l.Printf("%s%s%s %s", Green, DoneTag, Reset, message)
}

// DoneNonStreamLog logs non-streaming completions with green color
func (l *Logger) DoneNonStreamLog(format string, v ...interface{}) {
	message := fmt.Sprintf(format, v...)
	l.Printf("%s%s%s %s", Green, DoneNonStreamTag, Reset, message)
}

// SeparatorLog prints a dimmed separator line
func (l *Logger) SeparatorLog() {
	l.Printf("%s%s%s", Dim, Separator, Reset)
}

// ErrorLog logs errors with red color
func (l *Logger) ErrorLog(format string, v ...interface{}) {
	message := fmt.Sprintf(format, v...)
	l.Printf("%s⚠️  [ERROR] %s%s", Red, Reset, message)
}

// WarningLog logs warnings with yellow color
func (l *Logger) WarningLog(format string, v ...interface{}) {
	message := fmt.Sprintf(format, v...)
	l.Printf("%s⚠️  [WARN] %s%s", Yellow, Reset, message)
}
