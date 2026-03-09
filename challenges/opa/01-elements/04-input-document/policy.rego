package access

import rego.v1


username := input.user
user_action := input.action
user_region := input.enviroment.region
first_tag := input.tags[0]


allow_alice := true if {
  input.user == "alice"
}


allow_read := true if {
  input.action == "read"
  input.environment.region == "us-east-1"
}


is_confidential := true if {
  "confidential" in input.tags
}
