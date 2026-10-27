package agenvo

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/felinics/memoh/internal/deliverymeta"
)

// TaskInputDirectory derives a Windows destination from the selected immutable
// target root. A server-side NAS path is never interpreted as a PC path.
func TaskInputDirectory(target Target, p deliverymeta.InputPlan) (string, error) {
	if !p.Valid() || p.Task.RootHash != deliverymeta.RootHash(target.WorkDir) {
		return "", ErrUnsupported
	}
	if _, err := resolveWorkDir(target.WorkDir, "."); err != nil {
		return "", err
	}
	return strings.TrimRight(target.WorkDir, `\/`) + `\` + strings.ReplaceAll(p.Directory(), "/", `\`), nil
}

func taskInputContext(p BatchProgress, target Target) ([]byte, error) {
	if !p.valid() || p.Inputs == nil || p.Inputs.Task.RootHash != deliverymeta.RootHash(target.WorkDir) {
		return nil, ErrUnsupported
	}
	type file struct {
		deliverymeta.Input
		Path         string `json:"path"`
		RelativePath string `json:"relative_path"`
	}
	context := struct {
		Version int               `json:"version"`
		Task    deliverymeta.Task `json:"task"`
		Root    string            `json:"root"`
		Files   []file            `json:"files"`
	}{Version: 1, Task: p.Inputs.Task, Root: target.WorkDir}
	for _, f := range p.Inputs.Files {
		relative := p.Inputs.Path(f)
		separator := `\`
		if strings.HasPrefix(target.WorkDir, "/") {
			separator = "/" // Internal Linux generator fixtures; native target validation remains Windows-only.
		}
		absolute := strings.TrimRight(target.WorkDir, `\/`) + separator + strings.ReplaceAll(relative, "/", separator)
		context.Files = append(context.Files, file{Input: f, Path: absolute, RelativePath: relative})
	}
	return json.Marshal(context)
}

func inputEnvelope(script string, p BatchProgress, target Target) (string, error) {
	if p.Inputs == nil {
		return script, nil
	}
	raw, err := taskInputContext(p, target)
	if err != nil {
		return "", err
	}
	setup := fmt.Sprintf(`printf '%%s' '%s' | base64 --decode > "$memoh_attempt/input-context.json"
export MEMOH_TASK_INPUT_CONTEXT_FILE="$(cygpath -w "$memoh_attempt/input-context.json")"
memoh_inputs_ok=true
`, base64.StdEncoding.EncodeToString(raw))
	// Node is already required by the pi-json completion/history envelope. Its
	// native lstat recognizes Windows junctions, unlike a POSIX leaf-only -L.
	contextHash := sha256.Sum256(raw)
	setup += fmt.Sprintf(`if ! node -e "eval(Buffer.from('%s','base64').toString('utf8'))" "$MEMOH_TASK_INPUT_CONTEXT_FILE" '%x'; then memoh_inputs_ok=false; fi
`, base64.StdEncoding.EncodeToString([]byte(taskInputVerifier)), contextHash)
	script = strings.Replace(script, `mkdir "$memoh_attempt" || exit 125`, `mkdir "$memoh_attempt" || exit 125`+"\n"+setup, 1)
	script = strings.Replace(script, `if cd "$memoh_cwd" &&`, `if [ "$memoh_inputs_ok" = true ] && cd "$memoh_cwd" &&`, 1)
	return script, nil
}

const taskInputVerifier = `
const fs=require('fs'), path=require('path'), crypto=require('crypto');
try {
 const raw=fs.readFileSync(process.argv[1]);
 if(crypto.createHash('sha256').update(raw).digest('hex')!==process.argv[2]) throw 0;
 const p=JSON.parse(raw.toString('utf8'));
 const norm=s=>{s=path.normalize(s); if(process.platform==='win32') {s=s.replace(/^\\\\\?\\UNC\\/i,'\\\\').replace(/^\\\\\?\\/,''); s=s.toLowerCase();} return s.replace(/[\\/]+$/,'');};
 if(crypto.createHash('sha256').update(p.root.replace(/\//g,'\\').replace(/\\+$/,'').toLowerCase()).digest('hex')!==p.task.root_hash) throw 0;
 function checked(relative) {
  if(typeof relative!=='string' || !/^\.memoh-computer\/inputs\/[a-f0-9]{32}\/[a-f0-9]{64}$/.test(relative)) throw 0;
  let current=p.root;
  if(norm(fs.realpathSync.native(current))!==norm(current) || fs.lstatSync(current).isSymbolicLink()) throw 0;
  const parts=relative.split('/');
  for(let i=0;i<parts.length;i++) {current=path.join(current,parts[i]); const s=fs.lstatSync(current); if(s.isSymbolicLink() || (i<parts.length-1 && !s.isDirectory())) throw 0;}
  if(norm(fs.realpathSync.native(current))!==norm(current)) throw 0;
  return current;
 }
 for(const f of p.files) {
  if(f.relative_path!=='.memoh-computer/inputs/'+p.task.attempt+'/'+f.content_hash) throw 0;
  const name=checked(f.relative_path), before=fs.lstatSync(name);
  if(norm(name)!==norm(f.path)) throw 0;
  const fd=fs.openSync(name,fs.constants.O_RDONLY|(fs.constants.O_NOFOLLOW||0));
  try {
   const stat=fs.fstatSync(fd); if(!stat.isFile() || stat.nlink!==1 || stat.size!==f.size_bytes || stat.dev!==before.dev || stat.ino!==before.ino) throw 0;
   const hash=crypto.createHash('sha256'), buffer=Buffer.alloc(65536); let total=0,n;
   while((n=fs.readSync(fd,buffer,0,buffer.length,null))!==0) {total+=n; if(total>f.size_bytes) throw 0; hash.update(buffer.subarray(0,n));}
   if(total!==f.size_bytes || hash.digest('hex')!==f.content_hash) throw 0;
   const after=fs.lstatSync(checked(f.relative_path)); if(after.dev!==stat.dev || after.ino!==stat.ino || after.size!==stat.size || after.mtimeMs!==stat.mtimeMs) throw 0;
  } finally {fs.closeSync(fd);}
 }
} catch {process.exit(125);}
`
