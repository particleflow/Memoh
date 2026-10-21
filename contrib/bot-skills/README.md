# Opt-in Bot workflow skills

`windows-computer/SKILL.md` is a reusable procedure for authorized Windows command and local-agent work. It contains no device identifiers, endpoints, private paths or credentials. Repository files here are **not automatically installed** into any Bot.

For an individually selected Bot, use Memoh's existing authenticated Skills upload API (`POST /bots/{bot_id}/container/skills`, body `{"skills":["<complete SKILL.md text>"]}`), which requires workspace-write permission. The managed location is `/data/skills/windows-computer/SKILL.md` on that Bot's native Server Workspace, not on the connected Windows computer. The normal skill discovery exposes metadata; `use_skill` loads full instructions on demand. Installing the skill does not configure a target, connector, Pi or Magpie and does not grant access. The skill requires those separate capabilities to be discovered at execution time.

This branch also updates the native execution-location tool's usage guidance, which is injected only when the tool is registered. That basic discovery guidance does not require installing this skill. The detailed Herdr/Pi procedure becomes available only after a deliberate per-Bot installation. Neither change is deployed merely by creating this branch.

Keep real computer roots, Git Bash executable paths, Pi configuration directories, provider endpoints and secrets in private deployment configuration. The instructions use root-relative business paths and current discovery results. Memoh's existing native workspace and authorization checks remain in place; the Agenvo command target remains foreground execution only.
