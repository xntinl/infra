
package nodefault

import rego.v1


allow if {
  input.role == "admin"
}


allow if {
    input.role == "editor"
    input.action == "read"
}


