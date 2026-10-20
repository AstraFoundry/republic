package republic

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

func obj(v any) map[string]any {
	if m, ok := v.(map[string]any); ok && m != nil {
		return m
	}
	return map[string]any{}
}
func arr(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	return nil
}
func str(v any) string { s, _ := v.(string); return s }
func num(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return 0
}
func jsonText(v any) string { b, _ := json.Marshal(v); return string(b) }
func nonempty(v any) bool {
	if v == nil {
		return false
	}
	switch x := v.(type) {
	case string:
		return x != ""
	case map[string]any:
		return len(x) > 0
	case []any:
		return len(x) > 0
	case bool:
		return x
	}
	return true
}
func first(values ...any) any {
	for _, v := range values {
		if nonempty(v) {
			return v
		}
	}
	return nil
}
func errorPayload(data map[string]any) error {
	if nonempty(data["error"]) {
		return failure(ErrAPIResponse, "%s", jsonText(data["error"]))
	}
	return nil
}
func usageFields(v map[string]any, names map[string]string) map[string]int {
	r := map[string]int{}
	for from, to := range names {
		if x, ok := v[from]; ok && x != nil {
			r[to] = num(x)
		}
	}
	return r
}
func info(data map[string]any, finish string) Delta {
	return Delta{ID: str(data["id"]), Model: str(data["model"]), FinishReason: finish}
}
func reason(v any, mapping map[string]string) string {
	s := str(v)
	if s == "" {
		return ""
	}
	if r, ok := mapping[s]; ok {
		return r
	}
	return "other"
}
func opaque(format string, m map[string]any) Delta {
	p := ProviderData(format, cloneMap(m))
	return Delta{ProviderData: &p}
}
func citation(m map[string]any) Delta {
	return Delta{Event: &Event{Type: "citation_added", Citation: &Citation{URL: str(m["url"]), Title: str(m["title"]), CitedText: str(m["cited_text"])}}}
}
func builtin(name, id string, input map[string]any, output any) Delta {
	return Delta{Event: &Event{Type: "builtin_tool_call_ready", BuiltinCall: &BuiltinToolCall{Name: name, ID: id, Input: input, Output: output}}}
}
func imageDelta(mediaType, data string) (Delta, error) {
	b, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return Delta{}, err
	}
	p := Part{Type: "image", MediaType: mediaType, Data: b}
	return Delta{Event: &Event{Type: "image_ready", Image: &p}}, nil
}
func chatUsage(u map[string]any, responses bool) Delta {
	in, out := "prompt_tokens", "completion_tokens"
	if responses {
		in, out = "input_tokens", "output_tokens"
	}
	r := usageFields(u, map[string]string{in: "input_tokens", out: "output_tokens"})
	for k, v := range usageFields(obj(u[in+"_details"]), map[string]string{"cached_tokens": "cached_tokens"}) {
		r[k] = v
	}
	for k, v := range usageFields(obj(u[out+"_details"]), map[string]string{"reasoning_tokens": "reasoning_tokens"}) {
		r[k] = v
	}
	return Delta{Usage: r}
}
func messagesUsage(u map[string]any) Delta {
	r := usageFields(u, map[string]string{"input_tokens": "input_tokens", "output_tokens": "output_tokens", "cache_read_input_tokens": "cached_tokens", "cache_creation_input_tokens": "cache_write_tokens"})
	if _, ok := r["input_tokens"]; ok {
		r["input_tokens"] += r["cached_tokens"] + r["cache_write_tokens"]
	}
	return Delta{Usage: r}
}

// StreamParser converts one wire protocol into deltas, retaining per-stream state.
// A parser must never be shared by independent requests.
type StreamParser interface {
	Feed(event, data string) ([]Delta, error)
	Finish() error
}
type parser struct {
	format, dialect string
	completed       bool
	streamedCalls   map[string]bool
	blocks          map[int]map[string]any
	blockInputs     map[int]string
	toolIndexes     map[int]bool
	builtinUses     map[string]map[string]any
	pending         []map[string]any
	texts           map[int]map[string]any
	callIndex       int
	pendingCode     map[string]any
}

func NewStreamParser(format string) (StreamParser, error) { return newParser(format, "") }
func newParser(format, dialect string) (*parser, error) {
	switch format {
	case "chat", "responses", "messages", "gemini":
	default:
		return nil, failure(ErrUnsupportedFormat, "unknown chat format %q", format)
	}
	return &parser{format: format, dialect: dialect, streamedCalls: map[string]bool{}, blocks: map[int]map[string]any{}, blockInputs: map[int]string{}, toolIndexes: map[int]bool{}, builtinUses: map[string]map[string]any{}, texts: map[int]map[string]any{}}, nil
}
func (p *parser) Finish() error {
	if !p.completed {
		return failure(ErrStreamIncomplete, "stream ended without a completion signal")
	}
	return nil
}
func (p *parser) Feed(event, data string) ([]Delta, error) {
	if data == "[DONE]" && (p.format == "chat" || p.format == "responses") {
		p.completed = true
		return nil, nil
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(data), &v); err != nil {
		return nil, failure(ErrAPIResponse, "invalid %s stream JSON: %v", p.format, err)
	}
	switch p.format {
	case "chat":
		return p.chat(v, true)
	case "responses":
		return p.responsesStream(v)
	case "messages":
		return p.messagesStream(v)
	case "gemini":
		return p.gemini(v)
	}
	return nil, nil
}

// ParseChat parses a complete JSON response, including metadata needed for replay.
func ParseChat(format string, data []byte) (Response, error) {
	p, err := newParser(format, "")
	if err != nil {
		return Response{}, err
	}
	ds, err := p.parse(data)
	if err != nil {
		return Response{}, err
	}
	b := ResponseBuilder{}
	for _, d := range ds {
		b.Add(d)
	}
	return b.Response(), nil
}
func (p *parser) parse(data []byte) ([]Delta, error) {
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, failure(ErrAPIResponse, "invalid %s response JSON: %v", p.format, err)
	}
	if err := errorPayload(v); err != nil {
		return nil, err
	}
	switch p.format {
	case "chat":
		return p.chat(v, false)
	case "responses":
		ds := []Delta{}
		for _, item := range arr(v["output"]) {
			d, err := p.responseItem(obj(item), false)
			if err != nil {
				return nil, err
			}
			ds = append(ds, d...)
		}
		return append(ds, responseMeta(v)...), nil
	case "messages":
		if _, ok := v["content"].([]any); !ok {
			return nil, failure(ErrAPIResponse, "Messages response has no content array")
		}
		ds := []Delta{}
		for _, x := range arr(v["content"]) {
			block := obj(x)
			switch str(block["type"]) {
			case "text":
				p.pending = append(p.pending, map[string]any{"type": "text", "text": str(block["text"])})
				ds = append(ds, chunkDelta("text_delta", str(block["text"])))
				ds = append(ds, messageCitations(arr(block["citations"]))...)
			case "tool_use":
				p.pending = append(p.pending, map[string]any{"type": "tool_use", "id": block["id"]})
				ds = append(ds, Delta{Fragment: &ToolCallFragment{Key: str(block["id"]), ID: str(block["id"]), Name: str(block["name"]), Arguments: jsonText(block["input"]), Done: true}})
			default:
				ds = append(ds, p.flushOrder()...)
				ds = append(ds, p.roundTrip(block, true)...)
			}
		}
		ds = append(ds, messageStop(v)...)
		return append(ds, info(v, ""), messagesUsage(obj(v["usage"]))), nil
	case "gemini":
		return p.gemini(v)
	}
	return nil, nil
}
func (p *parser) chat(v map[string]any, stream bool) ([]Delta, error) {
	if err := errorPayload(v); err != nil {
		return nil, err
	}
	choices := arr(v["choices"])
	if !stream && len(choices) == 0 {
		return nil, failure(ErrAPIResponse, "Chat response has no choices")
	}
	if !stream {
		choices = choices[:1]
	}
	ds := []Delta{}
	for _, x := range choices {
		choice := obj(x)
		m := obj(choice["message"])
		if stream {
			m = obj(choice["delta"])
		}
		reasoning := str(first(m["reasoning_content"], m["reasoning"]))
		text := str(m["content"])
		if p.dialect == "minimax" && reasoning == "" {
			for _, detail := range arr(m["reasoning_details"]) {
				reasoning += str(obj(detail)["text"])
			}
		}
		if p.dialect == "mistral" {
			for _, x := range arr(m["content"]) {
				c := obj(x)
				if c["type"] == "text" {
					text += str(c["text"])
				}
				if c["type"] == "thinking" {
					for _, y := range arr(c["thinking"]) {
						t := obj(y)
						if t["type"] == "text" {
							reasoning += str(t["text"])
						}
					}
				}
			}
		}
		if reasoning != "" {
			ds = append(ds, chunkDelta("reasoning_delta", reasoning))
		}
		if text != "" {
			ds = append(ds, chunkDelta("text_delta", text))
		}
		for _, a := range arr(m["annotations"]) {
			an := obj(a)
			if an["type"] == "url_citation" {
				ds = append(ds, citation(obj(an["url_citation"])))
			}
		}
		if s := str(m["refusal"]); s != "" {
			ds = append(ds, chunkDelta("refusal_delta", s))
		}
		for _, x := range arr(m["images"]) {
			u := str(obj(obj(x)["image_url"])["url"])
			part, err := Image(u, "image/png")
			if err != nil {
				return nil, err
			}
			ds = append(ds, Delta{Event: &Event{Type: "image_ready", Image: &part}})
		}
		for _, x := range arr(m["tool_calls"]) {
			c := obj(x)
			f := obj(c["function"])
			key := str(c["id"])
			if stream {
				key = fmt.Sprint(num(c["index"]))
			}
			ds = append(ds, Delta{Fragment: &ToolCallFragment{Key: key, ID: str(c["id"]), Name: str(f["name"]), Arguments: str(f["arguments"]), Done: !stream}})
		}
		ds = append(ds, info(v, reason(choice["finish_reason"], map[string]string{"stop": "stop", "length": "length", "tool_calls": "tool_calls", "function_call": "tool_calls", "content_filter": "content_filter"})))
	}
	if nonempty(v["usage"]) {
		ds = append(ds, chatUsage(obj(v["usage"]), false))
	}
	return ds, nil
}
func responseMeta(v map[string]any) []Delta {
	finish := ""
	if v["status"] == "completed" {
		finish = "stop"
	} else if v["status"] == "incomplete" {
		finish = reason(first(obj(v["incomplete_details"])["reason"], "unknown"), map[string]string{"max_output_tokens": "length", "content_filter": "content_filter"})
	}
	ds := []Delta{info(v, finish)}
	if nonempty(v["usage"]) {
		ds = append(ds, chatUsage(obj(v["usage"]), true))
	}
	return ds
}
func callKey(v map[string]any) string { return str(first(v["id"], v["call_id"])) }
func responseCitations(a []any) []Delta {
	ds := []Delta{}
	for _, x := range a {
		v := obj(x)
		if v["type"] == "url_citation" {
			ds = append(ds, citation(v))
		}
	}
	return ds
}
func (p *parser) responseItem(item map[string]any, streamed bool) ([]Delta, error) {
	ds := []Delta{}
	kind := str(item["type"])
	switch kind {
	case "message":
		if !streamed {
			for _, x := range arr(item["content"]) {
				c := obj(x)
				if c["type"] == "output_text" {
					ds = append(ds, chunkDelta("text_delta", str(c["text"])))
					ds = append(ds, responseCitations(arr(c["annotations"]))...)
				} else if c["type"] == "refusal" {
					ds = append(ds, chunkDelta("refusal_delta", str(c["refusal"])))
				}
			}
		}
	case "function_call":
		ds = append(ds, Delta{Fragment: &ToolCallFragment{Key: callKey(item), ID: str(item["call_id"]), Name: str(item["name"]), Arguments: str(item["arguments"]), Done: true}})
	case "reasoning":
		if !streamed {
			parts := []string{}
			for _, x := range append(append([]any{}, arr(item["summary"])...), arr(item["content"])...) {
				v := obj(x)
				if v["type"] == "summary_text" || v["type"] == "reasoning_text" {
					parts = append(parts, str(v["text"]))
				}
			}
			if s := strings.Join(parts, "\n\n"); s != "" {
				ds = append(ds, chunkDelta("reasoning_delta", s))
			}
		}
		ds = append(ds, opaque("responses", item))
	default:
		if kind == "" {
			return ds, nil
		}
		if kind == "image_generation_call" && nonempty(item["result"]) {
			d, err := imageDelta("image/"+str(first(item["output_format"], "png")), str(item["result"]))
			if err != nil {
				return nil, err
			}
			ds = append(ds, d)
		}
		if strings.HasSuffix(kind, "_call") {
			name := strings.TrimSuffix(kind, "_call")
			if name == "code_interpreter" {
				name = "code_execution"
			}
			input := cloneMap(item)
			for _, k := range []string{"id", "type", "status", "output", "outputs", "results", "result"} {
				delete(input, k)
			}
			var output any
			for _, k := range []string{"output", "outputs", "results"} {
				if v, ok := item[k]; ok {
					output = v
					break
				}
			}
			ds = append(ds, builtin(name, str(item["id"]), input, output))
		}
		ds = append(ds, opaque("responses", item))
	}
	return ds, nil
}
func (p *parser) responsesStream(v map[string]any) ([]Delta, error) {
	switch str(v["type"]) {
	case "response.output_text.delta":
		return []Delta{chunkDelta("text_delta", str(v["delta"]))}, nil
	case "response.output_text.annotation.added":
		return responseCitations([]any{v["annotation"]}), nil
	case "response.refusal.delta":
		return []Delta{chunkDelta("refusal_delta", str(v["delta"]))}, nil
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		return []Delta{chunkDelta("reasoning_delta", str(v["delta"]))}, nil
	case "response.reasoning_summary_part.added":
		if num(v["summary_index"]) > 0 {
			return []Delta{chunkDelta("reasoning_delta", "\n\n")}, nil
		}
	case "response.output_item.added":
		item := obj(v["item"])
		if item["type"] == "function_call" {
			key := callKey(item)
			p.streamedCalls[key] = true
			return []Delta{{Fragment: &ToolCallFragment{Key: key, ID: str(item["call_id"]), Name: str(item["name"])}}}, nil
		}
	case "response.function_call_arguments.delta":
		return []Delta{{Fragment: &ToolCallFragment{Key: str(v["item_id"]), Arguments: str(v["delta"])}}}, nil
	case "response.output_item.done":
		item := obj(v["item"])
		key := callKey(item)
		if item["type"] == "function_call" && p.streamedCalls[key] {
			return []Delta{{Fragment: &ToolCallFragment{Key: key, Done: true}}}, nil
		}
		return p.responseItem(item, true)
	case "response.created":
		return []Delta{info(obj(v["response"]), "")}, nil
	case "response.completed", "response.incomplete":
		p.completed = true
		return responseMeta(obj(v["response"])), nil
	case "response.failed":
		return nil, errorPayload(obj(v["response"]))
	case "error":
		return nil, failure(ErrAPIResponse, "%s", first(v["message"], jsonText(v)))
	}
	return nil, nil
}
func messageCitations(a []any) []Delta {
	ds := []Delta{}
	for _, x := range a {
		v := obj(x)
		if nonempty(v["url"]) {
			ds = append(ds, citation(v))
		}
	}
	return ds
}
func messageStop(v map[string]any) []Delta {
	s := str(v["stop_reason"])
	if s == "" {
		return nil
	}
	ds := []Delta{}
	if s == "refusal" {
		if text := str(obj(v["stop_details"])["explanation"]); text != "" {
			ds = append(ds, chunkDelta("refusal_delta", text))
		}
	}
	return append(ds, Delta{FinishReason: reason(s, map[string]string{"end_turn": "stop", "stop_sequence": "stop", "max_tokens": "length", "model_context_window_exceeded": "length", "tool_use": "tool_calls", "refusal": "refusal", "pause_turn": "pause"})})
}
func (p *parser) flushOrder() []Delta {
	ds := []Delta{}
	for _, v := range p.pending {
		if v["type"] != "text" || nonempty(v["text"]) {
			ds = append(ds, opaque("messages", v))
		}
	}
	p.pending = nil
	return ds
}
func (p *parser) roundTrip(block map[string]any, includeReasoning bool) []Delta {
	ds := []Delta{}
	if s := str(block["thinking"]); s != "" && includeReasoning {
		ds = append(ds, chunkDelta("reasoning_delta", s))
	}
	if block["type"] == "server_tool_use" || block["type"] == "mcp_tool_use" {
		p.builtinUses[str(block["id"])] = block
	} else if use := p.builtinUses[str(block["tool_use_id"])]; use != nil {
		delete(p.builtinUses, str(block["tool_use_id"]))
		name := str(use["name"])
		if strings.HasSuffix(name, "code_execution") {
			name = "code_execution"
		}
		ds = append(ds, builtin(name, str(use["id"]), obj(use["input"]), block["content"]))
	}
	return append(ds, opaque("messages", block))
}
func (p *parser) messagesStream(v map[string]any) ([]Delta, error) {
	index := num(v["index"])
	key := fmt.Sprint(index)
	ds := []Delta{}
	switch str(v["type"]) {
	case "message_stop":
		p.completed = true
	case "message_start":
		m := obj(v["message"])
		ds = append(ds, info(m, ""), messagesUsage(obj(m["usage"])))
	case "content_block_start":
		block := obj(v["content_block"])
		switch str(block["type"]) {
		case "text":
			marker := map[string]any{"type": "text", "text": str(block["text"])}
			p.pending = append(p.pending, marker)
			p.texts[index] = marker
			if nonempty(block["text"]) {
				ds = append(ds, chunkDelta("text_delta", str(block["text"])))
			}
		case "tool_use":
			p.pending = append(p.pending, map[string]any{"type": "tool_use", "id": block["id"]})
			p.toolIndexes[index] = true
			ds = append(ds, Delta{Fragment: &ToolCallFragment{Key: key, ID: str(block["id"]), Name: str(block["name"])}})
		default:
			p.blocks[index] = cloneMap(block)
		}
	case "content_block_delta":
		d := obj(v["delta"])
		switch str(d["type"]) {
		case "text_delta":
			if marker := p.texts[index]; marker != nil {
				marker["text"] = str(marker["text"]) + str(d["text"])
			}
			ds = append(ds, chunkDelta("text_delta", str(d["text"])))
		case "citations_delta":
			ds = append(ds, messageCitations([]any{d["citation"]})...)
		case "input_json_delta":
			if p.toolIndexes[index] {
				ds = append(ds, Delta{Fragment: &ToolCallFragment{Key: key, Arguments: str(d["partial_json"])}})
			} else {
				p.blockInputs[index] += str(d["partial_json"])
			}
		case "thinking_delta", "signature_delta":
			block := p.blocks[index]
			if block == nil {
				return nil, failure(ErrAPIResponse, "delta for missing content block %d", index)
			}
			field := "signature"
			if d["type"] == "thinking_delta" {
				field = "thinking"
				ds = append(ds, chunkDelta("reasoning_delta", str(d["thinking"])))
			}
			block[field] = str(block[field]) + str(d[field])
		}
	case "content_block_stop":
		if p.toolIndexes[index] {
			ds = append(ds, Delta{Fragment: &ToolCallFragment{Key: key, Done: true}})
		}
		if block := p.blocks[index]; block != nil {
			delete(p.blocks, index)
			if s, ok := p.blockInputs[index]; ok {
				delete(p.blockInputs, index)
				if s == "" {
					s = "{}"
				}
				var input any
				if err := json.Unmarshal([]byte(s), &input); err != nil {
					return nil, err
				}
				block["input"] = input
			}
			ds = append(ds, p.flushOrder()...)
			ds = append(ds, p.roundTrip(block, false)...)
		}
	case "message_delta":
		ds = append(ds, messageStop(obj(v["delta"]))...)
		ds = append(ds, messagesUsage(obj(v["usage"])))
	case "error":
		return nil, errorPayload(v)
	}
	return ds, nil
}
func (p *parser) gemini(v map[string]any) ([]Delta, error) {
	if err := errorPayload(v); err != nil {
		return nil, err
	}
	candidates := arr(v["candidates"])
	if len(candidates) == 0 && nonempty(obj(v["promptFeedback"])["blockReason"]) {
		return nil, failure(ErrAPIResponse, "Prompt blocked: %s", obj(v["promptFeedback"])["blockReason"])
	}
	ds := []Delta{}
	finish := ""
	if len(candidates) > 0 {
		c := obj(candidates[0])
		for _, x := range arr(obj(c["content"])["parts"]) {
			part := obj(x)
			switch {
			case part["thought"] == true:
				if nonempty(part["text"]) {
					ds = append(ds, chunkDelta("reasoning_delta", str(part["text"])))
				}
			case part["text"] != nil:
				ds = append(ds, chunkDelta("text_delta", str(part["text"])))
			case nonempty(part["functionCall"]):
				call := obj(part["functionCall"])
				metadata := map[string]any{}
				id := str(call["id"])
				if id != "" {
					metadata["gemini_call_id"] = id
				} else {
					b := make([]byte, 16)
					if _, err := rand.Read(b); err != nil {
						return nil, err
					}
					id = "call_" + hex.EncodeToString(b)
				}
				if nonempty(part["thoughtSignature"]) {
					metadata["gemini_thought_signature"] = part["thoughtSignature"]
				}
				args := call["args"]
				if !nonempty(args) {
					args = map[string]any{}
				}
				ds = append(ds, Delta{Fragment: &ToolCallFragment{Key: fmt.Sprint(p.callIndex), ID: id, Name: str(call["name"]), Arguments: jsonText(args), Metadata: metadata, Done: true}})
				p.callIndex++
			case strings.HasPrefix(str(obj(part["inlineData"])["mimeType"]), "image/"):
				inline := obj(part["inlineData"])
				d, err := imageDelta(str(inline["mimeType"]), str(inline["data"]))
				if err != nil {
					return nil, err
				}
				ds = append(ds, d)
			case part["executableCode"] != nil:
				p.pendingCode = obj(part["executableCode"])
				ds = append(ds, opaque("gemini", part))
			case part["codeExecutionResult"] != nil:
				ds = append(ds, builtin("code_execution", "", obj(p.pendingCode), part["codeExecutionResult"]), opaque("gemini", part))
				p.pendingCode = nil
			}
		}
		ground := obj(c["groundingMetadata"])
		if nonempty(ground["webSearchQueries"]) {
			ds = append(ds, builtin("web_search", "", map[string]any{"queries": ground["webSearchQueries"]}, nil))
		}
		for _, x := range arr(ground["groundingChunks"]) {
			w := obj(obj(x)["web"])
			if nonempty(w) {
				ds = append(ds, citation(map[string]any{"url": w["uri"], "title": w["title"]}))
			}
		}
		if urls := arr(obj(c["urlContextMetadata"])["urlMetadata"]); len(urls) > 0 {
			list := []any{}
			for _, x := range urls {
				list = append(list, obj(x)["retrievedUrl"])
			}
			ds = append(ds, builtin("web_fetch", "", map[string]any{"urls": list}, urls))
		}
		if nonempty(c["finishReason"]) {
			p.completed = true
			finish = reason(c["finishReason"], map[string]string{"STOP": "stop", "MAX_TOKENS": "length", "SAFETY": "content_filter", "RECITATION": "content_filter", "BLOCKLIST": "content_filter", "PROHIBITED_CONTENT": "content_filter", "SPII": "content_filter", "IMAGE_SAFETY": "content_filter", "IMAGE_PROHIBITED_CONTENT": "content_filter"})
		}
	}
	ds = append(ds, Delta{ID: str(v["responseId"]), Model: str(v["modelVersion"]), FinishReason: finish})
	if nonempty(v["usageMetadata"]) {
		u := obj(v["usageMetadata"])
		r := usageFields(u, map[string]string{"promptTokenCount": "input_tokens", "cachedContentTokenCount": "cached_tokens"})
		r["reasoning_tokens"] = num(u["thoughtsTokenCount"])
		r["output_tokens"] = num(u["candidatesTokenCount"]) + r["reasoning_tokens"]
		ds = append(ds, Delta{Usage: r})
	}
	return ds, nil
}
