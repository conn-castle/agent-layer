package herdr

import "io"

// Handle consumes one native hook event. It is a strict no-op unless it is
// running in an actual HerdR pane and has a recognized main-session event.
func Handle(provider string, in io.Reader, out, errOut io.Writer, environ []string) error {
	return HandleForRoot(provider, "", in, out, errOut, environ)
}
