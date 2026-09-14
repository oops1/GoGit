package pack

func NameHash(name string) uint32 {
	return AppendNameHash(0, name)
}

func AppendNameHash(sum uint32, name string) uint32 {
	for i := range len(name) {
		switch current := name[i]; current {
		case ' ', '\t', '\n', '\r':
		default:
			sum = sum>>2 + uint32(current)<<24
		}
	}
	return sum
}
