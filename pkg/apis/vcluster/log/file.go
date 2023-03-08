package vlog

import (
	"bufio"
	"os"
	"path/filepath"

	. "github.com/onsi/gomega"

	"github.com/go-logr/logr"
)

// Provides a log_file that can be used to store the logs generated by clusterctl actions.
type OpenLogFileInput struct {
	LogFolder string
	Name      string
}

func OpenLogFile(input OpenLogFileInput) *LogFile {
	filePath := filepath.Join(input.LogFolder, input.Name)
	Expect(os.MkdirAll(filepath.Dir(filePath), 0750)).To(Succeed(), "Failed to create log folder %s", filepath.Dir(filePath))

	f, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666) //nolint:gosec // No security issue: filepath is safe.
	Expect(err).ToNot(HaveOccurred(), "Failed to create log file %s", filePath)

	return &LogFile{
		name:   input.Name,
		file:   f,
		Writer: bufio.NewWriter(f),
	}
}

type LogFile struct {
	name string
	file *os.File
	*bufio.Writer
}

func (f *LogFile) Name() string {
	return f.name
}

func (f *LogFile) Flush() {
	Expect(f.Writer.Flush()).To(Succeed(), "Failed to flush log %s", f.name)
}

func (f *LogFile) Close() {
	f.Flush()
	Expect(f.file.Close()).To(Succeed(), "Failed to close log %s", f.name)
}

func (f *LogFile) Logger() logr.Logger {
	return logr.New(&logger{writer: f})
}
