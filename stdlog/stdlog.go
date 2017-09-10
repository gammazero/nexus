/*
Package stdlog provides a minimal logging interface to allow nexus to use
nearly any logging implementation.

*/
package stdlog

// StdLog is a minimal interface implemented by nearly every logging package.
// The nexus package uses this interface for all logging, which allows nexus
// to use any logging package desired.
type StdLog interface {
	// Print logs a message.  Arguments are handled in the manner of fmt.Print.
	Print(v ...interface{})

	// Println logs a message.  Arguments are handled in the manner of
	// fmt.Println.
	Println(v ...interface{})

	// Printf logs a message.  Arguments are handled in the manner of
	// fmt.Printf.
	Printf(format string, v ...interface{})
}
