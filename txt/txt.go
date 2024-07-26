package txt

import "golang.org/x/exp/rand"

var Avatars = []string{
	"😀", "😎", "🤖", "👽", "🐱", "🐶", "🦄", "🐸", "🦉", "🦋",
	"🌈", "🌟", "🍎", "🍕", "🎸", "🚀", "🧙", "🧛", "🧜", "🧚",
}

func RandomAvatar() string {
	rnd := rand.Intn(len(Avatars))
	return Avatars[rnd]
}
