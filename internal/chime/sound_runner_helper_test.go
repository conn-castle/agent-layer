package chime

// soundRunnerFunc adapts a function to SoundRunner.
type soundRunnerFunc func() error

// Play starts the function-backed notification sound.
func (f soundRunnerFunc) Play() error { return f() }
