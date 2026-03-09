package demo

import rego.v1

servers := ["web","db", "cache"]

ports[name] := port if {
    some i, name in servers
    ports_list := [80,5432,6379]
    port := ports_list[i]
  }
}
