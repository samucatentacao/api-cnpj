package model

// Descrições oficiais RFB para códigos sem tabela de domínio dedicada na API.
func PorteDescricao(codigo string) string {
	switch codigo {
	case "00":
		return "NÃO INFORMADO"
	case "01":
		return "MICRO EMPRESA"
	case "03":
		return "EMPRESA DE PEQUENO PORTE"
	case "05":
		return "DEMAIS"
	default:
		return ""
	}
}

func SituacaoCadastralDescricao(codigo string) string {
	switch codigo {
	case "01":
		return "NULA"
	case "02":
		return "ATIVA"
	case "03":
		return "SUSPENSA"
	case "04":
		return "INAPTA"
	case "08":
		return "BAIXADA"
	default:
		return ""
	}
}

func IdentificadorMatrizFilialDescricao(codigo string) string {
	switch codigo {
	case "1":
		return "MATRIZ"
	case "2":
		return "FILIAL"
	default:
		return ""
	}
}

func IdentificadorSocioDescricao(codigo string) string {
	switch codigo {
	case "1":
		return "PESSOA JURÍDICA"
	case "2":
		return "PESSOA FÍSICA"
	case "3":
		return "ESTRANGEIRO"
	default:
		return ""
	}
}

func FaixaEtariaDescricao(codigo string) string {
	switch codigo {
	case "1":
		return "0 a 12 anos"
	case "2":
		return "13 a 20 anos"
	case "3":
		return "21 a 30 anos"
	case "4":
		return "31 a 40 anos"
	case "5":
		return "41 a 50 anos"
	case "6":
		return "51 a 60 anos"
	case "7":
		return "61 a 70 anos"
	case "8":
		return "71 a 80 anos"
	case "9":
		return "maior de 80 anos"
	default:
		return ""
	}
}
