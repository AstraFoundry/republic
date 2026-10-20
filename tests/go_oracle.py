"""Run the unchanged Python implementation as the Go migration's test oracle."""

import asyncio
import dataclasses
import json
from pathlib import Path

from republic.formats import _API_FORMATS
from republic.formats._base import ChatApiFormat, ResponseBuilder
from republic.formats._sse import iter_events


async def lines(text):
    for line in text.splitlines():
        yield line


async def main():
    root = Path(__file__).parent / "test_provider_golden" / "cases"
    results = {}
    for path in sorted(root.glob("*/*")):
        if path.suffix not in {".json", ".sse"}:
            continue
        name = path.parent.name.split("_")[1]
        api_format = _API_FORMATS[name]
        assert isinstance(api_format, ChatApiFormat)
        builder = ResponseBuilder()
        events = []
        if path.suffix == ".json":
            for delta in api_format.parse_chat(json.loads(path.read_text())):
                events.extend(builder.add(delta))
        else:
            parser = api_format.stream_parser()
            async for event in iter_events(
                lines(path.read_text()),
                end_marker="[DONE]" if name in {"chat", "responses"} else None,
                yield_end_marker=True,
            ):
                for delta in parser.feed(event.event, event.data):
                    events.extend(builder.add(delta))
            parser.finish()
            events.extend(builder.release_tool_calls())
        result = builder.response()
        results[str(path.relative_to(root))] = {
            "message": result.message.to_dict(),
            "token_usage": dataclasses.asdict(result.token_usage),
            "finish_reason": result.finish_reason,
            "refusal": result.refusal,
            "citations": [dataclasses.asdict(c) for c in result.citations],
            "builtin_tool_calls": [dataclasses.asdict(c) for c in result.builtin_tool_calls],
            "id": result.id,
            "model": result.model,
            "event_types": [type(event).__name__ for event in events],
        }
    print(json.dumps(results))


asyncio.run(main())
