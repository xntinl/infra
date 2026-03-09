package demos.every

import rego.v1

all_above_five_fail if {
    nums := [2,4,6,7,8]
    every n in nums {
        n > 5
      }
  }

all_above_five_pass if {
    nums := [6,7,8,9,10]
    every n in nums {
        n > 5
      }
  }

all_positive_v1 if {
    nums := [1,2,3]
    every n in nums {
        n > 0
      }
  }



_any_negative if {
    nums := [1,2,3]
    some n in nums
    n <= 0
  }


all_positive_v2 if {
    not _any_negative
  }
