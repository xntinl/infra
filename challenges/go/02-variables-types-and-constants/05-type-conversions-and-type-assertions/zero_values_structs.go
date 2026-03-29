package main


import "fmt"


type User struct {
	Name string
	Age int
	Active bool
	Score float64
}


func main(){
	var u User
	fmt.Printf("User: %+v\n",u)
	fmt.Printf("Name is empty: %t\n",u.Name=="")
	fmt.Printf("Age is zero: %t\n",u.Age==0)
	fmt.Printf("Active is false: %t\n",u.Active==false)
	fmt.Printf("Score is zero: %t\n",u.Score==0.0)
}


