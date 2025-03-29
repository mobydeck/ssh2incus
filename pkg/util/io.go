package util

import (
	"io"
)

// PipeReader represents anything that can read (like gossh.Channel)
type PipeReader interface {
	Read(p []byte) (n int, err error)
}

// PipeWriter represents anything that can write (like io.WriteCloser)
type PipeWriter interface {
	Write(p []byte) (n int, err error)
	Close() error
}

// ErrorWriter represents anything that can handle error output
// Similar to gossh.Channel's Stderr() method
type ErrorWriter interface {
	Stderr() io.ReadWriter
}

// SetupPipes sets up bidirectional piping between a reader/writer and standard pipes
// Returns pipes for stdin and stderr with proper types
func SetupPipes[T interface {
	PipeReader
	PipeWriter
	ErrorWriter
}](channel T) (stdin io.ReadCloser, stderr io.WriteCloser, cleanup func()) {
	// Create pipes with correct orientation
	stdinReader, stdinWriter := io.Pipe()   // stdinWriter is for input to the process
	stderrReader, stderrWriter := io.Pipe() // stderrReader is for reading error output

	// Forward data from channel to stdinWriter
	go func(c T, w io.WriteCloser) {
		defer w.Close()
		io.Copy(w, c)
	}(channel, stdinWriter)

	// Forward data from stderrReader to channel's stderr
	go func(c T, e io.ReadCloser) {
		defer e.Close()
		io.Copy(c.Stderr(), e)
	}(channel, stderrReader)

	// Return cleanup function
	cleanup = func() {
		stdinReader.Close()
		stdinWriter.Close()
		stderrReader.Close()
		stderrWriter.Close()
	}

	return stdinReader, stderrWriter, cleanup
}

// Alternative version with more flexibility by using separate generics
func SetupFlexiblePipes[
	R PipeReader,
	E interface {
		Stderr() io.WriteCloser
	},
](reader R, errorWriter E) (stdin *io.PipeWriter, stderr *io.PipeReader, cleanup func()) {
	stdinReader, stdinWriter := io.Pipe()
	stderrReader, stderrWriter := io.Pipe()

	// Forward data from reader to stdinWriter
	go func(r R, w *io.PipeWriter) {
		defer w.Close()
		io.Copy(w, r)
	}(reader, stdinWriter)

	// Forward data from stderrReader to errorWriter's stderr
	go func(e E, er *io.PipeReader) {
		defer er.Close()
		io.Copy(e.Stderr(), er)
	}(errorWriter, stderrReader)

	// Return cleanup function
	cleanup = func() {
		stdinReader.Close()
		stdinWriter.Close()
		stderrReader.Close()
		stderrWriter.Close()
	}

	return stdinWriter, stderrReader, cleanup
}
