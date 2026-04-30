package server

import "testing"

func TestSplitAtDelimiterFraming(t *testing.T) {
	input := []byte("<rpc message-id=\"1\"></rpc>]]>]]>")
	advance, token, err := SplitAt(input, false)
	if err != nil {
		t.Fatalf("SplitAt returned error: %v", err)
	}
	if string(token) != "<rpc message-id=\"1\"></rpc>" {
		t.Fatalf("unexpected token: %q", string(token))
	}
	if advance != len(input) {
		t.Fatalf("unexpected advance: got %d want %d", advance, len(input))
	}
}

func TestSplitAtChunkedSingleChunk(t *testing.T) {
	input := []byte("\n#26\n<rpc message-id=\"1\"></rpc>\n##\n")
	advance, token, err := SplitAt(input, false)
	if err != nil {
		t.Fatalf("SplitAt returned error: %v", err)
	}
	if string(token) != "<rpc message-id=\"1\"></rpc>" {
		t.Fatalf("unexpected token: %q", string(token))
	}
	if advance != len(input) {
		t.Fatalf("unexpected advance: got %d want %d", advance, len(input))
	}
}

func TestSplitAtChunkedMultiChunk(t *testing.T) {
	input := []byte("\n#10\n<rpc messa\n#16\nge-id=\"1\"></rpc>\n##\n")
	advance, token, err := SplitAt(input, false)
	if err != nil {
		t.Fatalf("SplitAt returned error: %v", err)
	}
	if string(token) != "<rpc message-id=\"1\"></rpc>" {
		t.Fatalf("unexpected token: %q", string(token))
	}
	if advance != len(input) {
		t.Fatalf("unexpected advance: got %d want %d", advance, len(input))
	}
}

func TestSplitAtChunkedNeedMoreData(t *testing.T) {
	input := []byte("\n#26\n<rpc message-id=\"1\"></rpc>\n")
	advance, token, err := SplitAt(input, false)
	if err != nil {
		t.Fatalf("SplitAt returned error: %v", err)
	}
	if advance != 0 || token != nil {
		t.Fatalf("expected no token yet, got advance=%d token=%q", advance, string(token))
	}
}

func TestSplitAtChunkedInvalidChunkSize(t *testing.T) {
	input := []byte("\n#x\n<rpc/>\n##\n")
	_, _, err := SplitAt(input, false)
	if err == nil {
		t.Fatal("expected invalid chunk size error")
	}
}
