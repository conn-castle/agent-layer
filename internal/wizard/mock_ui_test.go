package wizard

type MockUI struct {
	SelectFunc      func(title string, options []string, current *string) error
	MultiSelectFunc func(title string, options []string, selected *[]string) error
	ConfirmFunc     func(title string, value *bool) error
	InputFunc       func(title string, value *string) error
	SecretInputFunc func(title string, value *string) error
	NoteFunc        func(title string, body string) error
}

func (m *MockUI) Select(title string, options []string, current *string) error {
	if m.SelectFunc != nil {
		return m.SelectFunc(title, options, current)
	}
	return nil
}

func (m *MockUI) MultiSelect(title string, options []string, selected *[]string) error {
	if m.MultiSelectFunc != nil {
		return m.MultiSelectFunc(title, options, selected)
	}
	return nil
}

func (m *MockUI) Confirm(title string, value *bool) error {
	if m.ConfirmFunc != nil {
		return m.ConfirmFunc(title, value)
	}
	return nil
}

func (m *MockUI) Input(title string, value *string) error {
	if m.InputFunc != nil {
		return m.InputFunc(title, value)
	}
	return nil
}

func (m *MockUI) SecretInput(title string, value *string) error {
	if m.SecretInputFunc != nil {
		return m.SecretInputFunc(title, value)
	}
	return nil
}

func (m *MockUI) Note(title string, body string) error {
	if m.NoteFunc != nil {
		return m.NoteFunc(title, body)
	}
	return nil
}
