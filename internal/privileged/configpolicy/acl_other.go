//go:build !darwin

package configpolicy

func rejectDarwinACL(string) error { return nil }
