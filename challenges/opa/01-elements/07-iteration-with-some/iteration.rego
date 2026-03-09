package iteration

import rego.v1


server_names contains name if {
    some server in input.servers
    name := server.name
}

server_with_index contains entry if {
    some i, server  in input.servers
    entry := sprintf("%d: %s",[i, server.name])
  }


admins contains user if {
    some user, role in input.permissions
    role == "admin"
}



high_port_servers contains name if {
    some server in input.servers
    server.port > 1000
    name := server.name
  }

tcp_servers contains name if {
    some server in input.servers
    server.protocol == "tcp"
    name := server.name
  }

port_allowed if {
    some port in input.allowed_ports
    port == 443
  }


servers_on_allowed_ports contains server.name if {
    some server in input.servers
    server.port in input.allowed_ports

  }
