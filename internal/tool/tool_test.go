package tool

import "testing"

func TestListStableOrder(t *testing.T) {
	r := NewChrootedRegistry(t.TempDir())
	names1 := make([]string, 0)
	for _, tool := range r.List() {
		names1 = append(names1, tool.Name)
	}
	names2 := make([]string, 0)
	for _, tool := range r.List() {
		names2 = append(names2, tool.Name)
	}
	if len(names1) < 2 {
		t.Fatal("expected tools")
	}
	for i := range names1 {
		if names1[i] != names2[i] {
			t.Fatalf("unstable order: %v vs %v", names1, names2)
		}
		if i > 0 && names1[i-1] > names1[i] {
			t.Fatalf("not sorted: %v", names1)
		}
	}
}

func TestIsReadOnly(t *testing.T) {
	if !IsReadOnly("read_file") || IsReadOnly("write_file") || IsReadOnly("bash") {
		t.Fatal("readonly classification wrong")
	}
}
