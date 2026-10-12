# Magpie

Use the `magpie` provider to call a local [Magpie](https://github.com/yetone/magpie) gateway, which routes each request to the service behind the selected model. It supports chat and decisions. Chat requests default to Chat Completions; Responses and Messages are also available.

## Get started

Install Republic using the [quickstart](../quickstart.md#install-republic), start Magpie, and replace `MODEL_ID` with a model from its catalog:

```python
import asyncio

import republic


async def main():
    model = republic.get_model("magpie:MODEL_ID")
    response = await model.chat("Say hello in one sentence.")
    print(response.text)


asyncio.run(main())
```

## Authentication

Magpie holds the upstream credentials, so you don't need to supply a key. Republic sends the default key `magpie` in the `Authorization: Bearer` header. Pass `api_key=` to send a different key, or `auth=` to replace the header.

## Model support

The default endpoint is `http://127.0.0.1:3425/v1`. Select `api_format="responses"` or `api_format="messages"` to use another format, and `get_decision_model()` for decision models served through the gateway.

See [configuration](../reference/configuration.md) for endpoint overrides and request options.
