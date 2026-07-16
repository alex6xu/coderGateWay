package mdchat

import "testing"

func TestParseHeadingRoles(t *testing.T) {
	md := `# 登录问题讨论

## User
如何实现 JWT 登录？

## Assistant
可以用 HS256 签发 token。

## 用户
还有刷新令牌吗？

## 助手
可以加 refresh token。
`
	r := Parse(md)
	if r.Title != "登录问题讨论" {
		t.Fatalf("title=%q", r.Title)
	}
	if len(r.Messages) != 4 {
		t.Fatalf("msgs=%d %#v", len(r.Messages), r.Messages)
	}
	if r.Messages[0].Role != "user" || r.Messages[1].Role != "assistant" {
		t.Fatalf("roles wrong: %#v", r.Messages)
	}
	if err := Validate(r); err != nil {
		t.Fatal(err)
	}
}

func TestParseBoldInline(t *testing.T) {
	md := `**User**: hello
**Assistant**: hi there
`
	r := Parse(md)
	if len(r.Messages) != 2 {
		t.Fatalf("got %#v", r.Messages)
	}
	if r.Messages[0].Content != "hello" || r.Messages[1].Content != "hi there" {
		t.Fatalf("content %#v", r.Messages)
	}
}

func TestParseQA(t *testing.T) {
	md := `问：什么是前缀缓存？
答：稳定前缀可复用 KV cache。
`
	r := Parse(md)
	if len(r.Messages) < 2 {
		t.Fatalf("got %#v", r.Messages)
	}
}
