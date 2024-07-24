package etc

import "github.com/nrednav/cuid2"

func Gensym() string {
	return cuid2.Generate()
}
