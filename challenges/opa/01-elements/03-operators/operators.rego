package operators

import rego.v1


greeting := "hello"
total := 10 + 20



is_five := true if {
  x := 5
  x == 5
}


is_positive := true if {
  total > 0
}



sum := 10 + 3
diff := 10 - 3
product := 10 * 3
quotient :=  10 / 3
remainder := 10 % 3


allowed_roles := {"admin","editor","viewer"}

is_valid_role := true if {
    "admin" in allowed_roles
}



