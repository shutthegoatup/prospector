package request

func Put(to, v *string) {
	if v != nil {
		*to = *v
	}
}
