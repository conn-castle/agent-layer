//go:build !unix

package agentdispatch

func suppressMCPSIGPIPE() func() { return func() {} }
