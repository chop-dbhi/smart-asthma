package main

import (
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

func parseToken(authHeader string) (*jwt.Token, error) {
	index := strings.Index(authHeader, "Bearer ")
	if index == 0 {
		authHeader = authHeader[len("Bearer "):]
	}

	// Parse the auth token
	token, _, err := new(jwt.Parser).ParseUnverified(authHeader, jwt.MapClaims{})
	if err != nil {
		return nil, err
	}

	return token, nil
}

func getIssuer(token *jwt.Token) (string, error) {
	var host string
	if claims, ok := token.Claims.(jwt.MapClaims); ok {
		// Extract the "iss" claim from the token payload
		iss := claims["iss"]
		if iss != nil {
			host, ok = iss.(string)
			if !ok {
				return "", fmt.Errorf("issuer (iss) not a valid string")
			}
		} else {
			return "", fmt.Errorf("invalid issuer (iss) value")
		}
	} else {
		return "", fmt.Errorf("issuer (iss) claim not found")
	}
	return host, nil
}
