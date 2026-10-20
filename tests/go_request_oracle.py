"""Export request cases from the Python API for live Go differential checks."""

import dataclasses
import json

from republic import ChatOptions, Message, Tool, get_provider, image, system, tool, user
from republic._content import Reasoning, Text, ToolCall
from republic.formats._base import ChatApiFormat, ChatRequest, OutputSchema, normalize
from republic.tools import CodeExecution, ImageGeneration, NativeTool, UserLocation, WebFetch, WebSearch

weather = Tool("weather", "Get weather", {"type": "object", "properties": {"city": {"type": "string"}}})
call = ToolCall("call_test", "weather", '{"city":"Paris"}', metadata={"gemini_thought_signature": "signed"})
schema = OutputSchema("Answer", {"type": "object", "properties": {"answer": {"type": "string"}}})
messages = [
    system("Be concise."),
    user("weather?"),
    Message("assistant", (Reasoning("Think"), Text("Checking")), tool_calls=(call,)),
    tool(call, "sunny"),
    user("next"),
]
scenarios: list[tuple[str, list[Message], ChatOptions]] = [
    ("plain", [user("hello")], {}),
    ("conversation", messages, {}),
    ("image", [user("describe", image(b"png", media_type="image/png"))], {}),
    ("tools", messages, {"tools": [weather], "tool_choice": weather}),
    ("parallel", messages, {"tools": [weather], "parallel_tool_calls": False}),
    ("reasoning", messages, {"reasoning_effort": "high", "include_reasoning": True}),
    ("zero", [user("hello")], {"temperature": 0, "max_tokens": 0, "top_p": 0}),
    ("extra", [user("hello")], {"extra_body": {"metadata": {"key": "value"}, "temperature": 0.7}}),
    ("schema", [user("answer")], {}),
    ("stop", [user("hello")], {"stop": ["end"]}),
    ("seed", [user("hello")], {"seed": 42}),
    ("top_k", [user("hello")], {"top_k": 5}),
    ("web_search", [user("search")], {"tools": [WebSearch()]}),
    (
        "web_search_settings",
        [user("search")],
        {
            "tools": [
                WebSearch(
                    max_uses=2,
                    allowed_domains=["example.org"],
                    blocked_domains=["invalid.org"],
                    user_location=UserLocation(city="Paris"),
                )
            ]
        },
    ),
    ("web_search_location", [user("search")], {"tools": [WebSearch(user_location=UserLocation(country="FR"))]}),
    ("web_fetch", [user("fetch")], {"tools": [WebFetch()]}),
    ("code_execution", [user("compute")], {"tools": [CodeExecution()]}),
    ("image_generation", [user("draw")], {"tools": [ImageGeneration()]}),
    ("native", [user("native")], {"tools": [NativeTool("chat", {"type": "custom"})]}),
]
providers = {
    "openai": ["chat", "responses"],
    "anthropic": ["messages"],
    "google": ["gemini"],
    "deepseek": ["chat", "messages", "responses"],
    "moonshot": ["chat"],
    "zai": ["chat"],
    "ollama": ["chat"],
    "together": ["chat"],
    "mistral": ["chat"],
    "minimax": ["chat", "messages"],
    "openrouter": ["chat"],
}
results = {}
for provider, formats in providers.items():
    for format_name in formats:
        api_format = get_provider(provider, api_format=format_name).get_model("test-model").api_format
        assert isinstance(api_format, ChatApiFormat)
        for name, conversation, options in scenarios:
            for stream in (False, True):
                output_schema = schema if name == "schema" else None
                request = ChatRequest("test-model", normalize(conversation), options, output_schema)
                entry = {
                    "provider": provider,
                    "format": format_name,
                    "messages": [m.to_dict() for m in conversation],
                    "options": {k: v for k, v in options.items() if k not in {"tools", "tool_choice"}},
                    "tools": name in {"tools", "parallel"},
                    "builtin_tools": [
                        {
                            **dataclasses.asdict(t),
                            "kind": {
                                "WebSearch": "web_search",
                                "WebFetch": "web_fetch",
                                "CodeExecution": "code_execution",
                                "ImageGeneration": "image_generation",
                                "NativeTool": "native",
                            }[type(t).__name__],
                        }
                        for t in options.get("tools", ())
                        if not isinstance(t, Tool)
                    ],
                    "named_choice": name == "tools",
                    "schema": None if output_schema is None else output_schema.schema,
                    "stream": stream,
                }
                try:
                    built = api_format.chat_request(request, stream=stream)
                    entry.update(path=built.path, body=built.body, params=built.params)
                except Exception as exc:
                    entry["error"] = type(exc).__name__
                results[f"{provider}/{format_name}/{name}/{stream}"] = entry
print(json.dumps(results))
