package inventory

import rego.v1

public_ips := { ip |
 some server in input.servers
 server.public_ip != null
 ip := server.public_ip
}



high_cpu_servers := [info |
  some server in input.servers
  server.cpu_percent > 80
  info := {
      "name": server.name,
      "cpu": server.cpu_percent,
    }
]

server_owners := { server.name: server.owner |
  some server in input.servers
}

servers_by_region := {region: names |
  some server in input.servers
  region := server.region
  names := { s.name |
    some s in input.servers
      s.region == region
    }
}


total_servers := count(input.servers)


avg_cpu := sum(cpu_values) / count(cpu_values) if {
    cpu_values := [s.cpu_percent | some s in input.servers]
    count(cpu_values) > 0
  }


max_cpu := max({s.cpu_percent | some s in input.servers})

avg_cpu_by_region := { region: avg |
  some server in input.servers
  region := server.region
  region_cpus := [ s.cpu_percent |
    some s in input.servers
    s.region == region
  ]
  avg := sum(region_cpus) / count(region_cpus)
}
