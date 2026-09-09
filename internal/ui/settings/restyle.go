package settings

import (
	"github.com/oops1/headless-gui/v3/widget"

	"github.com/oops1/gogit/internal/ui/style"
)

func (v *View) Restyle(t *widget.Theme) {
	p := style.Of(t)
	p.Fields(v.fields()...)
	p.Lists(v.lists()...)
	p.Quiet(v.quietButtons()...)
	p.Primary(v.okBtn, v.credentialEditOK, v.keyEditOK)
	p.Hints(v.credentialsStatus, v.sshStatus, v.credentialEditStatus, v.sshEditStatus)
}

func (v *View) fields() []*widget.TextInput {
	return []*widget.TextInput{
		v.search,
		v.pullStrategy,
		v.defaultRemote,
		v.credentialResource,
		v.credentialUsername,
		v.credentialSecret,
		v.sshHostInput,
		v.sshPathInput,
		v.sshPassphraseInput,
	}
}

func (v *View) lists() []*widget.Dropdown {
	return []*widget.Dropdown{v.language, v.theme, v.credentialSource, v.credentialType}
}

func (v *View) quietButtons() []*widget.Button {
	return []*widget.Button{
		v.cancelBtn,
		v.credentialEditCancel,
		v.keyEditCancel,
		v.credentialAddBtn,
		v.credentialEditBtn,
		v.credentialRemoveBtn,
		v.credentialTestBtn,
		v.masterPasswordBtn,
		v.credentialsUnlockBtn,
		v.sshAddBtn,
		v.sshEditBtn,
		v.sshBrowseBtn,
		v.sshRemoveBtn,
		v.sshTestBtn,
		v.sshUnlockBtn,
	}
}
