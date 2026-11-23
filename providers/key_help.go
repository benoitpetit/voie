package providers

// KeyHelp returns the exact steps to obtain credentials for a provider.
// URLs and flag names are checked against provider documentation; nothing is
// fetched at runtime.
func KeyHelp(name string) []string {
	switch name {
	case "openrouter":
		return []string{
			"Open https://openrouter.ai/keys and sign in.",
			"Create a key, copy it (starts with sk-or-v1-).",
			"Free models are limited to 50 requests/day without lifetime credits.",
		}
	case "orcarouter":
		return []string{
			"Open https://www.orcarouter.ai/console/keys and sign in with GitHub.",
			"Create a key (starts with sk-orca-). The GitHub account must be aged to use free models.",
			"Free tier: 50 requests/day, 10 rpm, workspace-wide limits.",
		}
	case "groq":
		return []string{"Open https://console.groq.com/keys and create an API key.", "Free tier: 30 rpm / 1000 requests per day."}
	case "google-ai-studio":
		return []string{"Open https://aistudio.google.com/apikey and create an API key.", "Note: unpaid data may be used for model training."}
	case "nvidia-nim":
		return []string{"Open https://build.nvidia.com, sign in and generate an API key.", "Phone verification may be required."}
	case "zai":
		return []string{"Open https://z.ai/manage-apikey/apikey-list and create a key.", "Use GLM Flash models for the zero-priced tier."}
	case "mistral":
		return []string{"Open https://console.mistral.ai/api-keys and create a key.", "Free mode is enabled by default for new accounts."}
	case "cloudflare-ai":
		return []string{
			"Open the Cloudflare dashboard → Workers AI → Use REST API.",
			"Copy the Account ID shown there.",
			"Create an API token with Account > Workers AI > Read (and Edit if needed).",
			"Both values are required: the token and the account ID.",
		}
	case "cohere-trial":
		return []string{"Open https://dashboard.cohere.com/api-keys and create a trial key.", "Trial keys do not expire; free quota is 1000 requests/month."}
	case "requesty":
		return []string{"Open https://app.requesty.ai/api-keys and create a key.", "Free plan: 200 requests/day on zero-priced models."}
	case "aihubmix":
		return []string{"Open https://console.aihubmix.com/workspaces/default/keys and create a key.", "Free IDs end with -free; 100 requests/day."}
	case "anyapi":
		return []string{"Sign up at https://dash.anyapi.ai, verify your email and create a key at https://dash.anyapi.ai/?page=api-keys.", "Free plan: 100,000 AnyTokens/day, no credit card.", "Keep reasoning_effort low: xhigh can burn ~60K reasoning tokens per turn."}
	case "beatapi":
		return []string{"Open https://beatapi.io/dashboard/apikeys and create a key.", "Zero balance and never-topped-up accounts are allowed.", "Free tier is 1 request/minute until the first top-up; 3 free model IDs."}
	case "iflytek-spark":
		return []string{"Claim the free quota at https://xinghuo.xfyun.cn/sparkapi.", "Get the APIPassword at https://console.xfyun.cn/ (different password per model version).", "Real-name verification with national ID and phone is required; overseas review can take 3 business days.", "Only the `lite` model is free and it has no tool calling."}
	case "vlm-run":
		return []string{"No key is needed: voie uses the documented anonymous `Bearer vlmrun`.", "For higher limits, create a free key at https://app.vlm.run/dashboard/settings/api-keys.", "Anonymous limits: 10/min, 30/hour, 100/day per IP.", "Only small chat models work anonymously; the tool-capable model requires a key."}
	case "opencode-zen", "opencode-go":
		return []string{"No upstream token needed: the local opencode-zen gateway is anonymous (IP-metered).", "The value asked by connect is only voie's local gateway key (default voie-local).", "OpenCode Go additionally needs a Go credential configured inside the gateway."}
	case "antigravity":
		return []string{"Install CLIProxyAPI, then run its Antigravity login (Google OAuth).", "voie stores only the local gateway key; the Google login stays in the gateway."}
	case "kiro":
		return []string{"Install kiro-cli and run `kiro-cli login` with your own account.", "Free tier: 50 credits/month; no API key is used."}
	case "cline":
		return []string{"Install the Cline CLI (Node 20-24), then run `cline auth`.", "Free models are only available through the CLI/IDE, not the Cline API.", "voie connect launches `cline auth` for you if you are not logged in."}
	case "codebuddy":
		return []string{"Create an API key at https://codebuddy.ai/profile/keys, or run `codebuddy login`.", "Free tier: 100 credits/month plus daily promo credits."}
	case "minimax-code":
		return []string{"Run `mcode login` with your MiniMax account.", "Free credits come from daily check-in (400-1000 points/day).", "voie connect launches the login for you if needed."}
	case "devin":
		return []string{"Run `devin auth login` with your Devin/Cognition account.", "voie connect checks `devin auth status` and launches the login for you."}
	case "qoder":
		return []string{"Create a personal access token at https://qoder.com/account/integrations.", "Set it as QODER_PERSONAL_ACCESS_TOKEN, or run `qoder login`."}
	case "copilot-cli":
		return []string{"Run `copilot login`, or use a fine-grained GitHub token with Copilot Requests permission.", "Copilot Free includes limited chat/agent usage."}
	case "cursor":
		return []string{"Run `agent login`, or create a key at https://cursor.com/dashboard/api (CURSOR_API_KEY).", "Cursor has no guaranteed free model tier for automated use."}
	}
	if spec, ok := ExternalGatewayByName(name); ok {
		account, _ := ExternalGatewayAccount(name)
		return []string{
			"Third-party gateway (reverse-engineered endpoints, ToS/account risk).",
			"Install with: voie install " + spec.Name,
			"Account required: " + account.Required + " — " + account.Kind,
			"Account setup: " + account.URL,
			"Client key: " + account.Key,
			spec.SetupHint,
			"Then call the model with: voie chat --model " + spec.Alias,
		}
	}
	return []string{"No credential instructions are registered for " + name + "."}
}

// KeyHelpProviders lists every provider with registered credential instructions.
func KeyHelpProviders() []string {
	names := append([]string(nil), connectableNames()...)
	for _, spec := range ExternalGateways {
		names = append(names, spec.Name)
	}
	return names
}

func connectableNames() []string {
	names := []string{
		"openrouter", "orcarouter", "groq", "google-ai-studio", "nvidia-nim", "zai", "mistral",
		"cloudflare-ai", "cohere-trial", "requesty", "aihubmix", "anyapi", "beatapi", "iflytek-spark", "vlm-run", "opencode-zen", "opencode-go",
		"antigravity", "kiro", "cline", "codebuddy", "minimax-code", "devin", "qoder", "copilot-cli", "cursor",
	}
	return names
}
