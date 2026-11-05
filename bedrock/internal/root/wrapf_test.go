package root

import (
	"errors"
	"testing"
)

// TestWrapfNil: a policy read or write that went through has no error, and wrapf leaves
// it nil instead of panicking as errors.Wrapf does on nil.
func TestWrapfNil(t *testing.T) {
	t.Parallel()

	if err := wrapf(nil, "resourcemanager.FoldersClient.GetIamPolicy(): %s", "folders/1"); err != nil {
		t.Errorf("wrapf(nil) = %v, want nil", err)
	}
	if err := wrapf(errors.New("denied"), "admin.IamClient.SetIamPolicy(): %s", "x@y"); err == nil {
		t.Error("wrapf(denied) = nil, want the wrapped error")
	}
}
