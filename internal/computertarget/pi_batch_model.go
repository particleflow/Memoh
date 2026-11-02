package computertarget

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// BatchModel carries the selected model's advertised capability for one
// invocation. Only facts the live catalog actually stated are present; nil
// fields keep the deployment's anchor entry value. It is data only, never a
// credential, a path, or a value supplied by a model.
type BatchModel struct {
	ID            string   `json:"model"`
	Provider      string   `json:"provider"`
	ContextWindow *int     `json:"context_window,omitempty"`
	MaxTokens     *int     `json:"max_tokens,omitempty"`
	Input         []string `json:"input,omitempty"`
}

// stated reports whether any capability is known. Without one the invocation
// keeps the shared deployment configuration, which is the pre-existing path.
func (m BatchModel) stated() bool {
	return m.ContextWindow != nil || m.MaxTokens != nil || len(m.Input) > 0
}

// Stated reports whether the live catalog stated any capability for this
// model. Exported so the task layer can decide whether to carry a snapshot.
func (m BatchModel) Stated() bool { return m.stated() }

func (m BatchModel) valid() bool {
	if !m.stated() {
		return true
	}
	if m.ID == "" || len(m.ID) > 256 || m.Provider == "" || len(m.Provider) > 128 || strings.ContainsAny(m.ID+m.Provider, "\x00\r\n") || strings.HasPrefix(m.ID, "-") || strings.HasPrefix(m.Provider, "-") {
		return false
	}
	if m.ContextWindow != nil && *m.ContextWindow <= 0 || m.MaxTokens != nil && *m.MaxTokens <= 0 {
		return false
	}
	for _, modality := range m.Input {
		if modality != "text" && modality != "image" {
			return false
		}
	}
	return true
}

// modelConfigProgram derives one per-task Pi configuration directory from the
// deployment's own models.json. Only the selected model entry is rewritten; the
// provider settings and every other entry are copied unchanged. The credential
// command keeps pointing at the shared directory, so no secret file is
// duplicated and concurrent tasks never write the shared configuration.
const modelConfigProgram = `
const fs=require('fs'),path=require('path');
const [shared,out,specFile]=process.argv.slice(2);
const spec=JSON.parse(fs.readFileSync(specFile,'utf8'));
const posix=shared.replace(/\\/g,'/');
const rewire=v=>typeof v==='string'?v.split('${PI_CODING_AGENT_DIR}').join(posix):v;
let cfg={providers:{}};
try{cfg=JSON.parse(fs.readFileSync(path.join(shared,'models.json'),'utf8'))}catch{}
if(!cfg||typeof cfg!=='object'||!cfg.providers||typeof cfg.providers!=='object') process.exit(2);
const provider=cfg.providers[spec.provider];
if(!provider||typeof provider!=='object') process.exit(2);
const models=Array.isArray(provider.models)?provider.models.slice():[];
if(models.length===0||!models[0]||typeof models[0]!=='object') process.exit(2);
const entry=JSON.parse(JSON.stringify(models[0]));
entry.id=spec.model;
entry.name=spec.model;
if(Number.isInteger(spec.context_window)&&spec.context_window>0) entry.contextWindow=spec.context_window;
if(Number.isInteger(spec.max_tokens)&&spec.max_tokens>0) entry.maxTokens=spec.max_tokens;
if(Array.isArray(spec.input)&&spec.input.length>0) entry.input=spec.input.filter(m=>m==='text'||m==='image');
// The chosen entry leads so Pi's providerModels[0] anchor is this model; any
// entry it replaces is dropped so the id appears once and --model stays exact.
provider.models=[entry,...models.filter(m=>!m||m.id!==spec.model)];
if(typeof provider.apiKey==='string') provider.apiKey=rewire(provider.apiKey);
if(provider.headers&&typeof provider.headers==='object') for(const k of Object.keys(provider.headers)) provider.headers[k]=rewire(provider.headers[k]);
fs.mkdirSync(out,{recursive:true});
fs.writeFileSync(path.join(out,'models.json'),JSON.stringify(cfg,null,2)+'\n');
try{fs.copyFileSync(path.join(shared,'settings.json'),path.join(out,'settings.json'))}catch{}
`

// modelConfigScriptParts writes the derivation program, its one-task spec and
// the per-task directory, then points Pi at it. A missing Node runtime or any
// derivation failure removes the directory so the invocation keeps the shared
// configuration: capability metadata never blocks a task.
func modelConfigScriptParts(target Target, model string, caps BatchModel) (string, error) {
	if !caps.stated() {
		return "", nil
	}
	caps.ID, caps.Provider = model, target.Agent.Provider
	if !caps.valid() {
		return "", ErrUnsupported
	}
	raw, err := json.Marshal(caps)
	if err != nil {
		return "", ErrUnsupported
	}
	var packed bytes.Buffer
	zipper := gzip.NewWriter(&packed)
	_, _ = zipper.Write([]byte(modelConfigProgram))
	_ = zipper.Close()
	program := fmt.Sprintf("eval(require('node:zlib').gunzipSync(Buffer.from('%s','base64')).toString('utf8'));\n", base64.StdEncoding.EncodeToString(packed.Bytes()))
	prepare := fmt.Sprintf(`printf '%%s' '%s' | base64 --decode > "$memoh_attempt/model-config.cjs"
printf '%%s' '%s' | base64 --decode > "$memoh_attempt/model-config.json"
memoh_agent_dir="$memoh_attempt/agent"
if command -v node >/dev/null && node "$memoh_attempt/model-config.cjs" "$(cygpath -w %s)" "$(cygpath -w "$memoh_agent_dir")" "$(cygpath -w "$memoh_attempt/model-config.json")"; then
  export PI_CODING_AGENT_DIR="$(cygpath -w "$memoh_agent_dir")"
else
  rm -rf "$memoh_agent_dir"
fi
`, base64.StdEncoding.EncodeToString([]byte(program)), base64.StdEncoding.EncodeToString(raw), shellData(target.Agent.ConfigDir))
	return prepare, nil
}
