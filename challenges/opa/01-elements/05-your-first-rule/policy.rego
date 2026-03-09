package myapp

import rego.v1

allow if {
  input.role == "admin"
}


greeting := "hello" if {
    input.lang == "en"
}

greeting := "hola" if {
    input.lang == "es"
}

can_edit if {
    input.role == "editor"
    input.resource == "article"
}


