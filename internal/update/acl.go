package update

import "strings"

// Права папки обновления (Windows): владелец — Администраторы, DACL
// защищён от наследования (P) и разрешает полный доступ только SYSTEM и
// Администраторам; подпапки и файлы наследуют то же (OICI).
const stageSDDL = "O:BAG:BAD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"

// trustedSIDs — кому можно писать в папку обновления.
var trustedSIDs = map[string]bool{"SY": true, "BA": true, "S-1-5-18": true, "S-1-5-32-544": true}

// SDDLTrusted решает по SDDL (владелец + DACL) папки, наша ли она: владелец
// — SYSTEM или Администраторы, DACL защищён от наследования, есть хотя бы
// одна запись, и все записи — «разрешить» только SYSTEM или Администраторам.
// Всё, чего не понимаем, — не наше.
func SDDLTrusted(sddl string) bool {
	owner, dacl, ok := splitSDDL(sddl)
	if !ok || !trustedSIDs[owner] {
		return false
	}
	i := strings.IndexByte(dacl, '(')
	if i < 0 {
		return false // пустой DACL или NO_ACCESS_CONTROL — нет
	}
	flags := dacl[:i]
	if strings.Contains(flags, "NO_ACCESS_CONTROL") || !strings.Contains(strings.ReplaceAll(flags, "AI", ""), "P") {
		return false
	}
	aces := dacl[i:]
	n := 0
	for aces != "" {
		if aces[0] != '(' {
			return false
		}
		j := strings.IndexByte(aces, ')')
		if j < 0 {
			return false
		}
		f := strings.Split(aces[1:j], ";")
		if len(f) != 6 || f[0] != "A" || !trustedSIDs[f[5]] {
			return false
		}
		n++
		aces = aces[j+1:]
	}
	return n > 0
}

// splitSDDL достаёт владельца и DACL; группу и SACL пропускает.
func splitSDDL(s string) (owner, dacl string, ok bool) {
	parts := map[byte]string{}
	for len(s) >= 2 {
		if s[1] != ':' {
			return "", "", false
		}
		tag := s[0]
		rest := s[2:]
		// Конец части — следующий «X:» вне скобок.
		end, depth := len(rest), 0
		for k := 0; k < len(rest); k++ {
			switch rest[k] {
			case '(':
				depth++
			case ')':
				depth--
			}
			if depth == 0 && k+1 < len(rest) && rest[k+1] == ':' && strings.IndexByte("OGDS", rest[k]) >= 0 && k > 0 {
				end = k
				break
			}
		}
		parts[tag] = rest[:end]
		s = rest[end:]
	}
	owner, hasO := parts['O']
	dacl, hasD := parts['D']
	return owner, dacl, hasO && hasD && s == ""
}
