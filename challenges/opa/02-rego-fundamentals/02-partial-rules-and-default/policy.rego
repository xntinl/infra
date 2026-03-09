package alerts

import rego.v1

allow if {
    count(violations) == 0
  }

violations contains msg if {
    some server in input.servers
    count(server.tags) == 0
    msg := sprintf("server '%s' has no tags",[server.name])
  }

violations contains msg if {
    some server in input.servers
    count(server.public_ip) != null
    msg := sprintf("server '%s' has exposed public IP: %s",[server.name,server.public_ip])
  }

violations contains msg if {
    some server in input.servers
    server.encryption == false
    msg := sprintf("server '%s' does not have encryption enabled",[server.name])
  }


violation_details[server.name] := details if {
    some server in input.servers
    problems := { msg |
        count(server.tags) == 0
        msg := "no tags"
      } | { msg |
          server.public_ip != null
          msg := "exposed public IP"
        } | { msg |
          server.encryption == false
          msg := "no encryption"
          }

      count(problems) > 0
      details := {
          "problems":problems,
          "problem_count" : count(problems)
        }
  }



