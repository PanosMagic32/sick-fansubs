import { ApiProperty } from '@nestjs/swagger';

export class CreateBlogPostDto {
    @ApiProperty({ type: String })
    readonly title!: string;

    @ApiProperty({ type: String })
    readonly subtitle!: string;

    @ApiProperty({ type: String })
    readonly description!: string;

    @ApiProperty({ type: String })
    readonly thumbnail!: string;

    @ApiProperty({ type: String })
    readonly downloadLink!: string;

    @ApiProperty({ type: String })
    readonly dateTimeCreated!: string;
}
