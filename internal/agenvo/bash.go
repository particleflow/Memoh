package agenvo

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// bashScript runs the user command in a separate Bash process. Its bounded
// capture drains excess output so truncation cannot inject SIGPIPE into the
// command or replace its exit status. stderr is ordinary merged output.
func bashScript(command, nonce string) string {
	body := base64.StdEncoding.EncodeToString([]byte(command))
	wrongHost := base64.StdEncoding.EncodeToString([]byte(`{"failure":"unsupported_bash_host"}`))
	missingTools := base64.StdEncoding.EncodeToString([]byte(`{"failure":"bash_tools_unavailable"}`))
	return fmt.Sprintf(`set +e
set -o pipefail
case "$(command uname -s 2>/dev/null)" in
  MINGW*_NT-*|MSYS_NT-*) ;;
  *) printf 'MEMOH_%[2]s:%[3]s:END_%[2]s\n'; exit 0 ;;
esac
for memoh_tool in base64 head cat; do
  if ! command -v "$memoh_tool" >/dev/null 2>&1; then
    printf 'MEMOH_%[2]s:%[4]s:END_%[2]s\n'; exit 0
  fi
done
memoh_body="$(printf '%%s' '%[1]s' | base64 --decode; printf '.')"
memoh_body="${memoh_body%%.}"
printf 'MEMOH_%[2]s:'
{
  "$BASH" --noprofile --norc -p -c "$memoh_body" </dev/null 2>&1 | {
    memoh_output="$(head -c 4096 | base64 -w 0)" || exit 125
    memoh_extra="$(head -c 1 | base64 -w 0)" || exit 125
    cat >/dev/null || exit 125
    memoh_truncated=false
    if [ -n "$memoh_extra" ]; then memoh_truncated=true; fi
    printf '{"output_base64":"%%s","truncated":%%s,' "$memoh_output" "$memoh_truncated"
  }
  memoh_codes=("${PIPESTATUS[@]}")
  if [ "${memoh_codes[1]}" -ne 0 ]; then printf '"exit_code":null}'; exit 0; fi
  printf '"exit_code":%%d}' "${memoh_codes[0]}"
} | base64 -w 0
printf ':END_%[2]s\n'
`, body, nonce, wrongHost, missingTools)
}

// bashCommand uses PowerShell solely as the existing Windows terminal's
// launcher. No user command is evaluated by PowerShell. The configured Bash
// receives the generated script as data and the user body uses Bash semantics.
// The fixed Bash bootstrap contains no embedded quotes, avoiding PowerShell
// 5.1's native-argument quote conversion. -p disables BASH_ENV/profile injection.
// The path is one safely quoted parameter. Nothing exits the interactive pane
// or changes its shell, profile or global configuration.
func bashCommand(command, nonce, bashPath string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(bashScript(command, nonce)))
	missing := base64.StdEncoding.EncodeToString([]byte(`{"failure":"bash_unavailable"}`))
	missingFrame := base64.StdEncoding.EncodeToString([]byte("MEMOH_" + nonce + ":" + missing + ":END_" + nonce))
	return fmt.Sprintf(`& { param([string]$memohBash) if ([IO.File]::Exists($memohBash)) { & $memohBash --noprofile --norc -p -c 'printf %%s %s | base64 --decode | /usr/bin/bash --noprofile --norc -p' } else { [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('%s')) } } '%s'`, encoded, missingFrame, strings.ReplaceAll(bashPath, "'", "''"))
}
