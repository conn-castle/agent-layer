package main

// stripArgsSeparator removes a standalone "--" and returns the args that should be
// forwarded to the underlying client. Arguments before "--" are preserved.
func stripArgsSeparator(args []string) []string {
	for i, arg := range args {
		if arg == "--" {
			passArgs := make([]string, 0, len(args)-1)
			passArgs = append(passArgs, args[:i]...)
			passArgs = append(passArgs, args[i+1:]...)
			return passArgs
		}
	}
	return append([]string{}, args...)
}
