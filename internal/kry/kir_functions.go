package kry

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
)

func kirFunctionTargets(program *Program, paths kirPathNames) map[*Function]string {
	targets := make(map[*Function]string, len(program.Functions))
	counts := make(map[string]int, len(program.Functions))
	for _, function := range program.Functions {
		if function != nil {
			counts[function.Name]++
		}
	}
	for _, function := range program.Functions {
		if function == nil {
			continue
		}
		if counts[function.Name] <= 1 {
			targets[function] = function.Name
			continue
		}
		parameters := make([]string, 0, len(function.Params))
		for _, parameter := range function.Params {
			parameters = append(parameters, typeSpecString(parameter.Type))
		}
		targets[function] = kirFunctionIdentity(function.Name, paths.name(function.Module), typeSpecString(function.Receiver), parameters)
	}
	return targets
}

func kirFunctionTargetFromDocument(function *KIRFunction) string {
	if function == nil {
		return ""
	}
	parameters := make([]string, 0, len(function.Params))
	for _, parameter := range function.Params {
		if parameter == nil {
			parameters = append(parameters, "")
			continue
		}
		parameters = append(parameters, parameter.Type)
	}
	return kirFunctionIdentity(function.Name, function.Module, function.Receiver, parameters)
}

func kirFunctionIdentity(name, module, receiver string, parameters []string) string {
	material := newKIRIdentityMaterial(module, receiver, name)
	for _, parameter := range parameters {
		material.add(parameter)
	}
	digest := sha256.Sum256(material.data)
	return fmt.Sprintf("%s@%s", name, hex.EncodeToString(digest[:]))
}

type kirIdentityMaterial struct {
	data []byte
}

func newKIRIdentityMaterial(parts ...string) *kirIdentityMaterial {
	material := &kirIdentityMaterial{}
	for _, part := range parts {
		material.add(part)
	}
	return material
}

func (material *kirIdentityMaterial) add(part string) {
	material.data = strconv.AppendInt(material.data, int64(len(part)), 10)
	material.data = append(material.data, ':')
	material.data = append(material.data, part...)
}
