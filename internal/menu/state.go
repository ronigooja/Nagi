package menu

import (
	"fmt"
	"github.com/ronigooja/Nagi/internal/i18n"
	"github.com/ronigooja/Nagi/internal/output"
)

type stateDefinition struct {
	id, english string
	style       textStyle
}

// Keys are typed protocol values, never user resource names or English sentences.
var menuStates = map[string]stateDefinition{
	"block":            {id: "menu.state.block", english: "block", style: styleWarning},
	"forward_original": {id: "menu.state.forward_original", english: "forward_original", style: styleWarning},
	"websocket_client": {id: "menu.state.websocket_client", english: "websocket_client", style: styleWarning},
	"websocket_server": {id: "menu.state.websocket_server", english: "websocket_server", style: styleWarning},
	"generated":        {id: "menu.state.generated", english: "generated", style: styleWarning},
	"trusted":          {id: "menu.state.trusted", english: "trusted", style: styleWarning},
	"untrusted":        {id: "menu.state.untrusted", english: "untrusted", style: styleWarning},
	"unsupported":      {id: "menu.state.unsupported", english: "unsupported", style: styleWarning},
	"starting":         {id: "menu.state.starting", english: "starting", style: styleWarning},
	"memory":           {id: "menu.state.memory", english: "memory", style: styleWarning},
	"offline":          {id: "menu.state.offline", english: "offline", style: styleWarning},

	"unknown":            {id: "menu.state.unknown", english: "unknown", style: styleWarning},
	"unavailable":        {id: "menu.state.unavailable", english: "unavailable", style: styleWarning},
	"available":          {id: "menu.state.available", english: "available", style: styleSuccess},
	"running":            {id: "menu.state.running", english: "running", style: styleSuccess},
	"stopped":            {id: "menu.state.stopped", english: "stopped", style: styleWarning},
	"yes":                {id: "menu.state.yes", english: "yes", style: styleSuccess},
	"no":                 {id: "menu.state.no", english: "no", style: styleWarning},
	"enabled":            {id: "menu.state.enabled", english: "enabled", style: styleSuccess},
	"disabled":           {id: "menu.state.disabled", english: "disabled", style: styleWarning},
	"active":             {id: "menu.state.active", english: "active", style: styleSuccess},
	"inactive":           {id: "menu.state.inactive", english: "inactive", style: styleWarning},
	"automatic":          {id: "menu.state.automatic", english: "automatic", style: styleSuccess},
	"fixed":              {id: "menu.state.fixed", english: "fixed", style: styleSuccess},
	"rule":               {id: "menu.state.rule", english: "rule", style: styleSuccess},
	"global":             {id: "menu.state.global", english: "global", style: styleSuccess},
	"direct":             {id: "menu.state.direct", english: "direct", style: styleSuccess},
	"user":               {id: "menu.state.user", english: "user", style: styleSuccess},
	"privileged":         {id: "menu.state.privileged", english: "privileged", style: styleSuccess},
	"configured":         {id: "menu.state.configured", english: "configured", style: styleSuccess},
	"missing":            {id: "menu.state.missing", english: "missing", style: styleWarning},
	"ok":                 {id: "menu.state.ok", english: "ok", style: styleSuccess},
	"failed":             {id: "menu.state.failed", english: "failed", style: styleError},
	"none":               {id: "menu.state.none", english: "none", style: styleWarning},
	"present":            {id: "menu.state.present", english: "present", style: styleSuccess},
	"absent":             {id: "menu.state.absent", english: "absent", style: styleWarning},
	"untested":           {id: "menu.state.untested", english: "untested", style: styleWarning},
	"pending":            {id: "menu.state.pending", english: "pending", style: styleWarning},
	"paused":             {id: "menu.state.paused", english: "paused", style: styleWarning},
	"recording":          {id: "menu.state.recording", english: "recording", style: styleSuccess},
	"stopping":           {id: "menu.state.stopping", english: "stopping", style: styleWarning},
	"read-only":          {id: "menu.state.read-only", english: "read-only", style: styleWarning},
	"ready":              {id: "menu.state.ready", english: "ready", style: styleSuccess},
	"not-installed":      {id: "menu.state.not-installed", english: "not-installed", style: styleWarning},
	"installed":          {id: "menu.state.installed", english: "installed", style: styleSuccess},
	"local":              {id: "menu.state.local", english: "local", style: styleSuccess},
	"inherited":          {id: "menu.state.inherited", english: "inherited", style: styleSuccess},
	"subscription":       {id: "menu.state.subscription", english: "subscription", style: styleSuccess},
	"provider":           {id: "menu.state.provider", english: "provider", style: styleSuccess},
	"group":              {id: "menu.state.group", english: "group", style: styleSuccess},
	"node":               {id: "menu.state.node", english: "node", style: styleSuccess},
	"header_set":         {id: "menu.state.header_set", english: "header_set", style: styleChoice},
	"header_append":      {id: "menu.state.header_append", english: "header_append", style: styleChoice},
	"header_delete":      {id: "menu.state.header_delete", english: "header_delete", style: styleChoice},
	"url_set":            {id: "menu.state.url_set", english: "url_set", style: styleChoice},
	"method_set":         {id: "menu.state.method_set", english: "method_set", style: styleChoice},
	"status_set":         {id: "menu.state.status_set", english: "status_set", style: styleChoice},
	"json_set":           {id: "menu.state.json_set", english: "json_set", style: styleChoice},
	"json_delete":        {id: "menu.state.json_delete", english: "json_delete", style: styleChoice},
	"body_set":           {id: "menu.state.body_set", english: "body_set", style: styleChoice},
	"text_replace":       {id: "menu.state.text_replace", english: "text_replace", style: styleChoice},
	"regex_replace":      {id: "menu.state.regex_replace", english: "regex_replace", style: styleChoice},
	"trailer_set":        {id: "menu.state.trailer_set", english: "trailer_set", style: styleChoice},
	"trailer_delete":     {id: "menu.state.trailer_delete", english: "trailer_delete", style: styleChoice},
	"script":             {id: "menu.state.script", english: "script", style: styleChoice},
	"request":            {id: "menu.state.request", english: "request", style: styleChoice},
	"response":           {id: "menu.state.response", english: "response", style: styleChoice},
	"exchange":           {id: "menu.state.exchange", english: "exchange", style: styleChoice},
	"websocket":          {id: "menu.state.websocket", english: "websocket", style: styleChoice},
	"websocket_request":  {id: "menu.state.websocket_request", english: "websocket_request", style: styleChoice},
	"websocket_response": {id: "menu.state.websocket_response", english: "websocket_response", style: styleChoice},
	"true":               {id: "menu.state.true", english: "true", style: styleSuccess},
	"false":              {id: "menu.state.false", english: "false", style: styleWarning},
	"stop":               {id: "menu.state.stop", english: "stop", style: styleChoice},
	"pause":              {id: "menu.state.pause", english: "pause", style: styleChoice},
	"resume":             {id: "menu.state.resume", english: "resume", style: styleChoice},
	"clear":              {id: "menu.state.clear", english: "clear", style: styleChoice},
	"start":              {id: "menu.state.start", english: "start", style: styleChoice},
	"import":             {id: "menu.state.import", english: "import", style: styleChoice},
	"export":             {id: "menu.state.export", english: "export", style: styleChoice},
	"enable":             {id: "menu.state.enable", english: "Enable", style: styleChoice},
	"disable":            {id: "menu.state.disable", english: "Disable", style: styleChoice},
	"trust":              {id: "menu.state.trust", english: "trust", style: styleChoice},
	"untrust":            {id: "menu.state.untrust", english: "untrust", style: styleChoice},
}

func menuState(language i18n.Context, value string) string {
	if entry, ok := menuStates[value]; ok {
		return menuText(language, entry.id, entry.english)
	}
	return value
}
func (s *Session) stateValue(value string) string {
	text := menuState(s.language, value)
	if entry, ok := menuStates[value]; ok {
		return s.styled(entry.style, output.RedactError(text))
	}
	return output.RedactError(text)
}
func (s *Session) statusSummary(result Result) string {
	if result.Code != 0 || result.Error != nil {
		return localizedstatusSummary(s.language, result)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return localizedstatusSummary(s.language, result)
	}
	state := "unknown"
	if running, ok := data["running"].(bool); ok {
		state = "stopped"
		if running {
			state = "running"
		}
	}
	profile, _ := data["profile"].(string)
	if profile == "" {
		return menuText(s.language, "menu.status.status_summary.mihomo", "Mihomo: ") + s.stateValue(state) + menuText(s.language, "menu.status.status_summary.profile_unknown", " | Profile: unknown")
	}
	return menuFormat(s.language, "menu.status.status_summary.mihomo_s_profile_s", "Mihomo: %s | Profile: %s", s.stateValue(state), s.styled(styleSelected, output.RedactError(profile)))
}
func (s *Session) selectProtocol(label string, values []string) (string, error) {
	labels := make([]string, len(values))
	for i, value := range values {
		labels[i] = menuState(s.language, value)
	}
	return s.selectChoices(label, values, labels)
}
func stateString(language i18n.Context, value any) string {
	if flag, ok := value.(bool); ok {
		if flag {
			return menuState(language, "yes")
		}
		return menuState(language, "no")
	}
	return menuState(language, fmt.Sprint(value))
}

func localizedProxyModePage(language i18n.Context) *Page {
	return localizedactionPage(language, menuText(language, "menu.mode.title", "Proxy Mode"),
		Item{ID: "mode.session", Label: menuText(language, "menu.mode.session", "For this session"), Next: func() *Page { return localizedmodePage(language, false) }},
		Item{ID: "mode.saved", Label: menuText(language, "menu.mode.saved", "Save for future starts"), Next: func() *Page { return localizedmodePage(language, true) }})
}
