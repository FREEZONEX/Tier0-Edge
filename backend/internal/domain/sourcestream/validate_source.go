package sourcestream

// ValidateSource validates configuration without connecting or persisting it.
func (s *Service) ValidateSource(input any) error {
	def, _, err := parseDocument[SourceDefinition](input)
	if err != nil {
		return err
	}
	return def.Validate()
}
