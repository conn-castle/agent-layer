package main

// stripArgsSeparator removes a standalone "--" and returns the args that should be
// forwarded to the underlying client. Arguments before "--" are preserved.
func stripArgsSeparator(args []string) []string {
	passArgs := []string{}
	for i, arg := range args {
		if arg == "--" {
			passArgs = append(passArgs, args[i+1:]...)
			break
		}
		passArgs = append(passArgs, arg)
	}
	return passArgs
}
