package response

import (
	"bytes"
	"testing"
)

func TestPutResponseBufferCapacityLimit(t *testing.T) {
	smallBuffer := bytes.NewBuffer(make([]byte, 0, maxPooledResponseBufferCapacity))
	if !putResponseBuffer(smallBuffer) {
		t.Fatal("small response buffer should be pooled")
	}

	largeBuffer := bytes.NewBuffer(make([]byte, 0, maxPooledResponseBufferCapacity+1))
	if putResponseBuffer(largeBuffer) {
		t.Fatal("large response buffer should not be pooled")
	}
}
