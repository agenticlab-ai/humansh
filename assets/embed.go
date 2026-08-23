package assets

import _ "embed"

//go:embed schema/translation-response.schema.json
var TranslationSchema []byte

//go:embed shell/zsh/humansh.zsh
var ZshIntegration []byte

//go:embed shell/bash/humansh.bash
var BashIntegration []byte
