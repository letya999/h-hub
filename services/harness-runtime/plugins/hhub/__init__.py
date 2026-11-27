"""Bridge Hermes's scoped model and tool callbacks to this container's adapter."""

import hashlib
import hmac
import json
import os
import urllib.request
from contextvars import ContextVar
from functools import partial

from hermes_scope import current_scope
from toolsets import create_custom_toolset

terminal_key = ContextVar("hhub_terminal_call_key", default="")


def admission_headers():
    scope = current_scope()
    turn = getattr(scope, "turn", "")
    if not turn:
        raise RuntimeError("No active Hermes admission")
    proof = hmac.new(os.environ["API_SERVER_KEY"].encode(), turn.encode(), hashlib.sha256)
    return {"X-HHub-Admission": turn, "X-HHub-Proof": proof.hexdigest()}


def model_request(catalog, request, **_):
    headers = admission_headers()
    query = urllib.request.Request("http://127.0.0.1:8643/tools", headers=headers)
    with urllib.request.urlopen(query, timeout=5) as response:
        allowed = json.load(response)
    body = {**request, "extra_headers": headers}
    if "tools" in body:
        body["tools"] = [tool for tool in body["tools"] if tool["function"]["name"] not in catalog
                         or tool["function"]["name"] in allowed]
        if not body["tools"]:
            body.pop("tools")
            body.pop("tool_choice", None)
    return {"request": body}


def terminal_call_identity(tool_name, next_call, tool_call_id="", **_):
    if tool_name != "terminal_exec":
        return next_call()
    token = terminal_key.set(tool_call_id)
    try:
        return next_call()
    finally:
        terminal_key.reset(token)


def invoke(name, args, **_):
    request = urllib.request.Request(
        "http://127.0.0.1:8643/tool",
        data=json.dumps({"name": name, "arguments": args, "key": terminal_key.get()}).encode(),
        headers={**admission_headers(), "Content-Type": "application/json"},
    )
    with urllib.request.urlopen(request, timeout=300) as response:
        return response.read().decode()


def register(ctx):
    with open("/etc/hhub/render/tools.json", encoding="utf-8") as source:
        projection = json.load(source)
    tools = projection.get("catalog") or []
    native = projection.get("native") or []
    create_custom_toolset("hhub_native", "Passport native tools", tools=native)
    create_custom_toolset("hhub_disabled", "Disabled native tools", tools=projection["denied_native"])
    managed = set(native).union(tool["name"] for tool in tools)
    ctx.register_middleware("llm_request", partial(model_request, managed))
    ctx.register_middleware("tool_execution", terminal_call_identity)
    for tool in tools:
        name = tool["name"]
        schema = {key: tool[key] for key in ("name", "description", "parameters")}
        handle = ctx.register_tool(name, "hhub", schema, partial(invoke, name))
        if handle is None:
            raise RuntimeError(f"Hermes refused to register projected tool {name}")
