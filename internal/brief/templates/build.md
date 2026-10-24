# Build {{spec_path}} in {{repo}}

You are the lead of spec {{spec_id}}, which the operator picked in `{{repo}}` (`{{repo_path}}`), dispatched {{date}}. You are in a fresh worktree of `{{repo_path}}` on your own branch. Follow the `buildermesh:lead` skill (installed as a plugin on this box). Your node is `{{node}}`: every `bmesh` command you run carries `--node {{node}}`, starting now with `bmesh --node {{node}} started --text "<one line: what you will build>"`.

## The spec, `{{spec_path}}`

----- begin spec -----
{{spec}}
----- end spec -----

{{operator_words}}## This repo

- Its definition of done, from its registry row: {{done_rule}}{{upgrading}}
