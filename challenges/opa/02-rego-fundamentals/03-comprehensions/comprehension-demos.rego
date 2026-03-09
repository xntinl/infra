package demos.comprehensions

import rego.v1

set_demo := { x |
  some x in [3,1,4,1,5,9,2,6,5]
  x > 3
}


array_demo := [ y |
  x := [1,2,3,5][_]
  y := x * 2
]

object_demo := { name: upper(name) |
some name in ["alice","bob","charlie"]
  }







