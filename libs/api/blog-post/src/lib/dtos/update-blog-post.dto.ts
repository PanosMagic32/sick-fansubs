import { ApiProperty, PartialType } from '@nestjs/swagger';

import { CreateBlogPostDto } from './create-blog-post.dto';

export class UpdateBlogPostDto extends PartialType(CreateBlogPostDto) {
  @ApiProperty({ type: String })
  override readonly title!: string;

  @ApiProperty({ type: String })
  override readonly subtitle!: string;

  @ApiProperty({ type: String })
  override readonly description!: string;

  @ApiProperty({ type: String })
  override readonly thumbnail!: string;

  @ApiProperty({ type: String })
  override readonly downloadLink!: string;

  @ApiProperty({ type: String })
  override readonly downloadLinkTorrent!: string;

  @ApiProperty({ type: String })
  override readonly downloadLink4k!: string;

  @ApiProperty({ type: String })
  override readonly downloadLink4kTorrent!: string;

  @ApiProperty({ type: String })
  override readonly dateTimeCreated!: string;
}
