package tags

import "testing"

func TestClassifyCodingAndDebug(t *testing.T) {
	hits := Classify("请帮我实现一个登录接口，用 JWT 鉴权")
	if len(hits) == 0 {
		t.Fatal("expected hits")
	}
	foundAuthOrCoding := false
	for _, h := range hits {
		if h.Slug == "auth" || h.Slug == "coding" || h.Slug == "backend" {
			foundAuthOrCoding = true
		}
	}
	if !foundAuthOrCoding {
		t.Fatalf("expected auth/coding/backend, got %#v", hits)
	}

	hits2 := Classify("这段代码报错了，帮我排查 bug")
	ok := false
	for _, h := range hits2 {
		if h.Slug == "debug" {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("expected debug, got %#v", hits2)
	}
}

func TestPreview(t *testing.T) {
	s := Preview("你好世界", 2)
	if s != "你好…" {
		t.Fatalf("got %q", s)
	}
}
