package install

// autoApprovePrompter returns a prompter that accepts all overwrite and deletion prompts.
func autoApprovePrompter() PromptFuncs {
	return PromptFuncs{
		OverwriteAllPreviewFunc:       func([]DiffPreview) (bool, error) { return true, nil },
		OverwriteAllMemoryPreviewFunc: func([]DiffPreview) (bool, error) { return true, nil },
		OverwritePreviewFunc:          func(DiffPreview) (bool, error) { return true, nil },
		DeleteUnknownAllFunc:          func([]string) (bool, error) { return true, nil },
		DeleteUnknownFunc:             func(string) (bool, error) { return true, nil },
	}
}
