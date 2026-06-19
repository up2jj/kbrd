package model

const mnemonicAlphabet = "asdfghjkl"

// GenerateMnemonics returns n unique, prefix-free tags drawn from the home-row
// alphabet, vimium-style: as few items as possible get long tags, no 1-char tag
// is a prefix of any 2-char tag. For n > len(alphabet)^2 the algorithm falls
// back to longer tags by recursing on the long-prefix bucket.
func GenerateMnemonics(n int) []string {
	return generateMnemonics(n, mnemonicAlphabet)
}

func generateMnemonics(n int, alphabet string) []string {
	if n <= 0 {
		return nil
	}
	k := len(alphabet)
	if n <= k {
		out := make([]string, n)
		for i := range n {
			out[i] = string(alphabet[i])
		}
		return out
	}

	// expansion = number of trailing 1-char slots that must be expanded into
	// k-many 2-char slots so that total capacity >= n.
	expansion := min(ceilDiv(n-k, k-1), k)

	short := alphabet[:k-expansion]
	longPrefixes := alphabet[k-expansion:]

	out := make([]string, 0, n)
	for i := 0; i < len(short); i++ {
		out = append(out, string(short[i]))
	}

	remaining := n - len(out)
	// How many tags must each prefix produce. With "expansion" prefixes and
	// "remaining" tags to place, distribute as evenly as possible.
	perPrefix := remaining / expansion
	extra := remaining % expansion

	for i := 0; i < expansion && remaining > 0; i++ {
		need := perPrefix
		if i < extra {
			need++
		}
		if need == 0 {
			continue
		}
		var sub []string
		if need <= k {
			sub = make([]string, need)
			for j := 0; j < need; j++ {
				sub[j] = string(alphabet[j])
			}
		} else {
			sub = generateMnemonics(need, alphabet)
		}
		prefix := string(longPrefixes[i])
		for _, s := range sub {
			out = append(out, prefix+s)
			remaining--
			if remaining == 0 {
				break
			}
		}
	}
	return out
}

func ceilDiv(a, b int) int {
	if a <= 0 {
		return 0
	}
	return (a + b - 1) / b
}
