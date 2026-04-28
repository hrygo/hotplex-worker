package feishu

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConvertMessage_TextWithMentions(t *testing.T) {
	t.Parallel()
	text, ok, medias := ConvertMessage("text", `{"text":"hello @user"}`, nil, "bot1", "msg1")
	require.True(t, ok)
	require.Contains(t, text, "hello")
	require.Nil(t, medias)
}

func TestConvertMessage_UnsupportedType(t *testing.T) {
	t.Parallel()
	text, ok, medias := ConvertMessage("share_chat", `{}`, nil, "", "")
	require.False(t, ok)
	require.Empty(t, text)
	require.Nil(t, medias)
}

func TestConvertMessage_Image_WithKey(t *testing.T) {
	t.Parallel()
	raw := `{"image_key":"img_abc"}`
	text, ok, medias := ConvertMessage("image", raw, nil, "", "msg1")
	require.True(t, ok)
	require.Contains(t, text, "图片")
	require.Len(t, medias, 1)
	require.Equal(t, "img_abc", medias[0].Key)
	require.Equal(t, "msg1", medias[0].MessageID)
}

func TestConvertMessage_File_WithKey(t *testing.T) {
	t.Parallel()
	raw := `{"file_name":"report.pdf","file_key":"file_abc"}`
	text, ok, medias := ConvertMessage("file", raw, nil, "", "msg1")
	require.True(t, ok)
	require.Contains(t, text, "文件")
	require.Len(t, medias, 1)
	require.Equal(t, "report.pdf", medias[0].Name)
}

func TestConvertMessage_Audio_WithKey(t *testing.T) {
	t.Parallel()
	raw := `{"file_key":"audio_abc"}`
	_, ok, medias := ConvertMessage("audio", raw, nil, "", "msg1")
	require.True(t, ok)
	require.Len(t, medias, 1)
	require.Equal(t, "audio", medias[0].Type)
}

func TestConvertMessage_Video_WithKey(t *testing.T) {
	t.Parallel()
	raw := `{"file_key":"vid_abc","file_name":"clip.mp4"}`
	_, ok, medias := ConvertMessage("video", raw, nil, "", "msg1")
	require.True(t, ok)
	require.Len(t, medias, 1)
	require.Equal(t, "clip.mp4", medias[0].Name)
}

func TestConvertMessage_Sticker_WithKey(t *testing.T) {
	t.Parallel()
	raw := `{"file_key":"stk_abc"}`
	_, ok, medias := ConvertMessage("sticker", raw, nil, "", "msg1")
	require.True(t, ok)
	require.Len(t, medias, 1)
	require.Equal(t, "sticker", medias[0].Type)
}

func TestConvertPost_WithLink(t *testing.T) {
	t.Parallel()
	raw := `{"content":[[{"tag":"a","text":"click","href":"https://example.com"}]]}`
	text, medias := convertPost(raw, nil, "", "")
	require.Contains(t, text, "[click](https://example.com)")
	require.Nil(t, medias)
}

func TestConvertPost_WithImageElement(t *testing.T) {
	t.Parallel()
	raw := `{"content":[[{"tag":"img","image_key":"img_xyz"}]]}`
	text, medias := convertPost(raw, nil, "", "msg_img")
	require.Contains(t, text, "[图片]")
	require.Len(t, medias, 1)
	require.Equal(t, "img_xyz", medias[0].Key)
}

func TestConvertPost_WithTitle(t *testing.T) {
	t.Parallel()
	raw := `{"title":"My Title","content":[[{"tag":"text","text":"body"}]]}`
	text, _ := convertPost(raw, nil, "", "")
	require.Contains(t, text, "## My Title")
	require.Contains(t, text, "body")
}

func TestConvertPostElement_LinkNoHref(t *testing.T) {
	t.Parallel()
	elem := postElement{Tag: "a", Text: "plain"}
	require.Equal(t, "plain", convertPostElement(elem, nil, ""))
}

func TestConvertPostElement_UnknownTag(t *testing.T) {
	t.Parallel()
	elem := postElement{Tag: "custom"}
	require.Equal(t, "", convertPostElement(elem, nil, ""))
}

func TestBuildMediaPrompt_TranscriptionOnly(t *testing.T) {
	t.Parallel()
	medias := []*MediaInfo{{Type: "audio", Key: "a1"}}
	result := BuildMediaPrompt("hello", nil, medias, []string{"你好世界"})
	require.Contains(t, result, "已转文字")
	require.Contains(t, result, "你好世界")
	require.Contains(t, result, "hello")
}

func TestBuildMediaPrompt_TranscriptionAndPaths(t *testing.T) {
	t.Parallel()
	medias := []*MediaInfo{{Type: "audio", Key: "a1"}}
	result := BuildMediaPrompt("", []string{"/tmp/a.wav"}, medias, []string{"hello"})
	require.Contains(t, result, "音频文件也已保存")
}

func TestBuildMediaPrompt_AllMediaTypes(t *testing.T) {
	t.Parallel()
	medias := []*MediaInfo{
		{Type: "image", Key: "i1"},
		{Type: "file", Key: "f1"},
		{Type: "audio", Key: "a1"},
		{Type: "video", Key: "v1"},
		{Type: "sticker", Key: "s1"},
	}
	result := BuildMediaPrompt("", []string{"/tmp/f"}, medias, nil)
	require.Contains(t, result, "1 张图片")
	require.Contains(t, result, "1 个文件")
	require.Contains(t, result, "1 条语音")
	require.Contains(t, result, "1 段视频")
	require.Contains(t, result, "1 个表情贴纸")
}

func TestBuildMediaPrompt_NoUserText(t *testing.T) {
	t.Parallel()
	medias := []*MediaInfo{{Type: "image", Key: "i1"}}
	result := BuildMediaPrompt("", []string{"/tmp/img.png"}, medias, nil)
	require.Contains(t, result, "已下载到本地")
	require.NotContains(t, result, "用户的文字内容")
}

func TestBuildInteractionCard_NoFooter(t *testing.T) {
	t.Parallel()
	result := buildInteractionCard("body text", "")
	require.Contains(t, result, "body text")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &parsed))
	// No hr element when no footer.
	body := parsed["body"].(map[string]any)
	elements := body["elements"].([]any)
	require.Len(t, elements, 1)
}

func TestStripInvalidImageKeys_NoImageSyntax(t *testing.T) {
	t.Parallel()
	result := StripInvalidImageKeys("hello world")
	require.Equal(t, "hello world", result)
}

func TestStripInvalidImageKeys_ValidKey(t *testing.T) {
	t.Parallel()
	result := StripInvalidImageKeys("![alt](img_abc123)")
	require.Equal(t, "![alt](img_abc123)", result)
}

func TestStripInvalidImageKeys_InvalidURL(t *testing.T) {
	t.Parallel()
	result := StripInvalidImageKeys("![alt](https://example.com/img.png)")
	require.Equal(t, "", result)
}

func TestStripInvalidImageKeys_InvalidKey(t *testing.T) {
	t.Parallel()
	result := StripInvalidImageKeys("see ![alt](file_abc) here")
	require.Equal(t, "see  here", result)
}
