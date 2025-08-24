package testutil

import (
	"bytes"
	"fmt"
)

type TestLogger struct {
	Buf *bytes.Buffer
}

func (l *TestLogger) Infof(format string, v ...any) {
	fmt.Fprintf(l.Buf, "INFO: "+format, v...)
}

func (l *TestLogger) Errorf(format string, v ...any) {
	fmt.Fprintf(l.Buf, "ERROR: "+format, v...)
}
