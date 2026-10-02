package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"birdcage/tools/internal/loopkit"
)

func spec() failureSpec {
	return failureSpec{Version: 1,
		Policies: []policy{{ID: "stop", GoName: "Stop", Meaning: "m"}, {ID: "retry", GoName: "Retry", Meaning: "m"}},
		Kinds:    []kind{{ID: "network", GoName: "Network", Policy: "retry", Message: "네트워크"}, {ID: "private", GoName: "Private", Policy: "stop", Message: "비공개"}}}
}

// 의미 해시: 설명(meaning/note)만 바뀌면 같고, 정책·메시지가 바뀌면 달라진다.
func TestSemanticHash(t *testing.T) {
	base := spec().Hash()
	s := spec()
	s.Policies[0].Meaning = "다른 설명"
	s.Kinds[0].Note = "주석"
	if s.Hash() != base {
		t.Error("description-only change must keep the semantic hash")
	}
	s = spec()
	s.Kinds[1].Policy = "retry"
	if s.Hash() == base {
		t.Error("policy change must change the semantic hash")
	}
	s = spec()
	s.Kinds[0].Message = "다른 메시지"
	if s.Hash() == base {
		t.Error("message change must change the semantic hash")
	}
}

func TestValidate(t *testing.T) {
	s := spec()
	s.Kinds[0].Policy = "nope"
	if err := s.validate(); err == nil || !strings.Contains(err.Error(), "unknown policy") {
		t.Errorf("unknown policy: %v", err)
	}
	s = spec()
	s.Kinds = append(s.Kinds, kind{ID: "network", GoName: "Net2", Policy: "stop", Message: "x"})
	if err := s.validate(); err == nil {
		t.Error("duplicate kind must fail")
	}
}

// 저장소의 실제 명세로 생성한 결과가 커밋된 생성물과 같아야 한다(= specgen -check).
func TestGeneratedFilesAreCurrent(t *testing.T) {
	root, err := loopkit.Root(".")
	if err != nil {
		t.Fatal(err)
	}
	outs, err := generate(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range outs {
		cur, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(o.rel)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(cur, o.data) {
			t.Errorf("%s is stale: run make gen", o.rel)
		}
		if !bytes.Contains(o.data, []byte("DO NOT EDIT")) || !bytes.Contains(o.data, []byte("semantic-hash: sha256:")) {
			t.Errorf("%s lacks generated header", o.rel)
		}
	}
}
