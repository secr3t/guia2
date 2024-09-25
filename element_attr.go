package guia2

import "strconv"

const (
	attrElementId     = "elementId"      //00000000-0000-0ad9-ffff-ffff000000a0
	attrIndex         = "index"          //0
	attrPackage       = "package"        //com.google.android
	attrClass         = "class"          //android.widget.LinearLayout
	attrText          = "text"           //some text
	attrResourceId    = "resource-id"    //resource id
	attrCheckable     = "checkable"      //true|false
	attrChecked       = "checked"        //true|false
	attrClickable     = "clickable"      //true|false
	attrEnabled       = "enabled"        //true|false
	attrFocusable     = "focusable"      //true|false
	attrLongClickable = "long-clickable" //true|false
	attrPassword      = "password"       //true|false
	attrScrollable    = "scrollable"     //true|false
	attrSelected      = "selected"       //true|false
	attrBounds        = "bounds"         //[0,0][1440,2733]
	attrDisplayed     = "displayed"      //true|false
)

func (e *Element) Index() (int, error) {
	if v, err := e.GetAttribute(attrIndex); err != nil {
		return 0, err
	} else {
		return strconv.Atoi(v)
	}
}

func (e *Element) Package() (string, error) {
	return e.GetAttribute(attrPackage)
}

func (e *Element) Class() (string, error) {
	return e.GetAttribute(attrClass)
}

func (e *Element) ResourceId() (string, error) {
	return e.GetAttribute(attrResourceId)
}

func (e *Element) booleanAttr(attrKey string) (bool, error) {
	if v, err := e.GetAttribute(attrKey); err != nil {
		return false, err
	} else {
		return v == "true", nil
	}
}

func (e *Element) Checkable() (bool, error) {
	return e.booleanAttr(attrCheckable)
}

func (e *Element) Checked() (bool, error) {
	return e.booleanAttr(attrChecked)
}

func (e *Element) Clickable() (bool, error) {
	return e.booleanAttr(attrClickable)
}

func (e *Element) Enabled() (bool, error) {
	return e.booleanAttr(attrEnabled)
}

func (e *Element) Focusable() (bool, error) {
	return e.booleanAttr(attrFocusable)
}

func (e *Element) LongClickable() (bool, error) {
	return e.booleanAttr(attrLongClickable)
}

func (e *Element) Password() (bool, error) {
	return e.booleanAttr(attrPassword)
}

func (e *Element) Scrollable() (bool, error) {
	return e.booleanAttr(attrScrollable)
}

func (e *Element) Selected() (bool, error) {
	return e.booleanAttr(attrSelected)
}

func (e *Element) IsDisplayed() (bool, error) {
	return e.booleanAttr(attrDisplayed)
}

func (e *Element) CanClick() bool {
	if enabled, err := e.Enabled(); err != nil || !enabled {
		return false
	}

	if displayed, err := e.IsDisplayed(); err != nil || !displayed {
		return false
	}

	if clickable, err := e.Clickable(); err != nil || !clickable {
		return false
	}

	return true
}
