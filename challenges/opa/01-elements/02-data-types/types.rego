package types

import rego.v1

my_string := "sdhello"
my_number := 42
my_float := 3.14
my_bool := true
my_null := null


my_array := [1,2,3]
my_object := {"name":"alice","age":10,"admin":true}
my_set := {"read","write","execute"}


first_element := my_array[0]
user_name := my_object.name
element_count := count(my_array)


string_type := type_name(my_string)
array_type := type_name(my_array)
object_type := type_name(my_object)
set_type := type_name(my_set)






