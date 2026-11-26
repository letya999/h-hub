"""Print the capability inventory of the running pinned image."""
import hashlib
import json
import sys

import model_tools  # loads the built-in registry
from gateway.config import Platform
from tools.registry import registry
from toolsets import get_toolset_names, resolve_toolset

surface = {
    "platforms": sorted(platform.value for platform in Platform),
    "tools": sorted(registry.get_all_tool_names()),
    "toolsets": {name: sorted(resolve_toolset(name)) for name in sorted(get_toolset_names())},
}
digest = hashlib.sha256(json.dumps(surface, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
names = set(surface["tools"]).union(*(set(tools) for tools in surface["toolsets"].values()))
print("source_pin:", json.dumps(sys.argv[1]))
print("inventory_sha256:", digest)
for key, values in (("platforms", surface["platforms"]), ("toolsets", surface["toolsets"]), ("tools", names)):
    print(f"{key}:")
    for value in sorted(values):
        print("  -", json.dumps(value))
