package provider

import "testing"

func TestNormalizeOpenAICompatibleBaseURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "https://api.openai.com/v1"},
		{"https://api.openai.com/v1", "https://api.openai.com/v1"},
		{"https://api.openai.com/v1/", "https://api.openai.com/v1"},
		{"https://api.openai.com", "https://api.openai.com/v1"},
		{"https://proxy.example.com", "https://proxy.example.com/v1"},
		{"https://proxy.example.com/", "https://proxy.example.com/v1"},
		{"https://proxy.example.com/v1/chat/completions", "https://proxy.example.com/v1"},
		{"https://open.bigmodel.cn/api/paas/v4", "https://open.bigmodel.cn/api/paas/v4"},
		{"https://generativelanguage.googleapis.com/v1beta", "https://generativelanguage.googleapis.com/v1beta"},
		{"https://my.azure.com/openai/deployments/x", "https://my.azure.com/openai/deployments/x"},
	}
	for _, tc := range cases {
		got := NormalizeOpenAICompatibleBaseURL(tc.in)
		if got != tc.want {
			t.Fatalf("NormalizeOpenAICompatibleBaseURL(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestJoinOpenAIURL(t *testing.T) {
	got := JoinOpenAIURL("https://api.openai.com/v1/", "/chat/completions")
	want := "https://api.openai.com/v1/chat/completions"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
