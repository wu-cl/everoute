package main

func main() {
	print(uuidToString("95d804961cc233280d0e68715a36653983c0b87b9cae57282efb82a0732449f6"))

}

func uuidToString(uuid string) uint32 {
	// return value lead with 1
	var ret uint32 = 1

	for index, char := range uuid {
		// 9 nums in return
		if index == 9 {
			break
		}
		num := int32(char - '0')
		if num >= 0 && num <= 9 {
			ret = ret*10 + uint32(num)
		}
	}

	return ret
}
