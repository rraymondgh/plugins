package utils

import (
	gonanoid "github.com/matoous/go-nanoid/v2"
	"go.uber.org/zap"
)

func NewRandom() string {
	id, err := gonanoid.Generate("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz", 22)
	if err != nil {
		zap.L().Error("Could not generate new ID", zap.Error(err))
	}

	return id
}

// func NewHash(data ...string) string {
// 	hash := md5.New()
// 	for _, d := range data {
// 		hash.Write([]byte(d))
// 		hash.Write([]byte(string('\u200b')))
// 	}
// 	h := hash.Sum(nil)
// 	bi := big.NewInt(0)
// 	bi.SetBytes(h)
// 	s := bi.Text(62)
// 	return fmt.Sprintf("%022s", s)
// }

// func NewTagID(name, value string) string {
// 	return NewHash(strings.ToLower(name), strings.ToLower(value))
// }
