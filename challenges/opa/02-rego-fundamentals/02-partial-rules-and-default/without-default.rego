package demo

import rego.v1

allow if {
    input.role == "admin"
  }
