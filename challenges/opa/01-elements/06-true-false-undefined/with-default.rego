package withdefault

import rego.v1


default allow := false

allow if {
    input.role == "admin"
  }

allow if {
    input.role == "editor"
    input.action == "read"
  }
