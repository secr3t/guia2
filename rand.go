package guia2

import (
	"crypto/rand"
	"math/big"
)

func RandomInt(x ...int) int {
	return int(RandomInt64(x...))
}

func RandomInt64(x ...int) int64 {
	var r *big.Int
	switch len(x) {
	case 0:
		r, _ = rand.Int(rand.Reader, big.NewInt(big.MaxExp))
	case 1:
		r, _ = rand.Int(rand.Reader, big.NewInt(int64(x[0])))
	case 2:
		if x[1] == x[0] {
			return int64(x[1])
		}
		r, _ = rand.Int(rand.Reader, big.NewInt(int64(x[1]-x[0])))
		return int64(x[0]) + r.Int64()
	}
	return r.Int64()
}
