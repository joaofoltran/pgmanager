package idgen

import (
	"crypto/rand"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

func NewClusterID() string {
	return "cl_" + strings.ToLower(ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String())
}

func NewNodeID() string {
	return "nd_" + strings.ToLower(ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String())
}
