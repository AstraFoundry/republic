from __future__ import annotations

from typing import TYPE_CHECKING, Unpack

from .base import Provider

if TYPE_CHECKING:
    from republic._registry import ProviderOptions


class Magpie(Provider):
    """A local Magpie gateway, which routes every API format to any model in its catalog."""

    name = "magpie"
    DEFAULT_API_BASE = "http://127.0.0.1:3425/v1"
    SUPPORTED_API_FORMATS = ("chat", "responses", "messages", "system_one")

    def __init__(self, **options: Unpack[ProviderOptions]) -> None:
        # Use the default magpie key so users don't have to supply one explicitly.
        options.setdefault("api_key", "magpie")
        super().__init__(**options)
