package utils

import (
	"hash/fnv"
)

var palette = []string{
	"#FF6B6B", "#4ECDC4", "#45B7D1", "#96CEB4", "#FFEAA7",
	"#DDA0DD", "#98D8C8", "#F7DC6F", "#BB8FCE", "#85C1E9",
	"#F0B27A", "#82E0AA", "#F1948A", "#85929E", "#73C6B6",
	"#E59866", "#A3E4D7", "#AED6F1", "#FAD7A0", "#A9CCE3",
	"#D7BDE2", "#A9DFBF", "#F9E79F", "#ABEBC6",
}

func GetUserColor(userID string) string {
	h := fnv.New32a()
	h.Write([]byte(userID))
	idx := int(h.Sum32()) % len(palette)
	return palette[idx]
}
