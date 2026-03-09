package demo.some_vs_every

import rego.v1

result if {
    nums := [2,4,6,7,8]
    some n in nums
    n > 5
  }


